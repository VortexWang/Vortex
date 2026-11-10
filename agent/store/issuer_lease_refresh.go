package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RefreshHubDelegationLeases renews only runtime/share authority already bound
// to a funded delegation. It cannot issue credit or change any source, amount,
// epoch, boot or hard deadline. FULL storage and its separate journal commit
// before transport. The lower clock bound must prove a complete60s interval.
func (s *Store) RefreshHubDelegationLeases(ctx context.Context, hub, node, boot, delegation string, now IssuerTime) (IssuedAuthorization, error) {
	return s.refreshHubRuntime(ctx, hub, node, boot, delegation, now, false)
}

// RefreshHubRuntimeHeartbeat persists a separate empty authorization envelope.
// HubStatus remains observation only. This10s evidence stream bridges the
// existing30s recovery boundary without creating or extending any authority.
func (s *Store) RefreshHubRuntimeHeartbeat(ctx context.Context, hub, node, boot, delegation string, now IssuerTime) (IssuedAuthorization, error) {
	return s.refreshHubRuntime(ctx, hub, node, boot, delegation, now, true)
}

func (s *Store) refreshHubRuntime(ctx context.Context, hub, node, boot, delegation string, now IssuerTime, heartbeatOnly bool) (IssuedAuthorization, error) {
	var zero IssuedAuthorization
	if s.owner != "hub" || !validIssuerUUID(hub) || !validIssuerUUID(node) || !validIssuerUUID(boot) || !validIssuerUUID(delegation) {
		return zero, ErrAuthorizationDenied
	}
	if e := checkIssuerTime(now); e != nil || now.Latest.Sub(now.Earliest) > 10*time.Second {
		return zero, ErrAuthorityTimeUncertain
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	var raw, digest []byte
	var state, epoch string
	if e = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,state,signer_epoch FROM issuer_delegations WHERE delegation_id=?`, delegation).Scan(&raw, &digest, &state, &epoch); e != nil {
		return zero, e
	}
	sum := sha256.Sum256(raw)
	var d HubDelegation
	if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &d) != nil || d.ID != delegation || d.HubID != hub || d.NodeID != node || d.BootID != boot || state != "open" || len(d.ShareReferences) != 3 {
		return zero, ErrAuthorizationDenied
	}
	d, e = effectiveHubDelegationTx(ctx, tx, d, digest, epoch, true)
	if e != nil {
		return zero, e
	}
	var signerEpoch, signerState string
	if e = tx.QueryRowContext(ctx, `SELECT signer_epoch,state FROM issuer_epochs WHERE hub_id=?`, hub).Scan(&signerEpoch, &signerState); e != nil {
		return zero, e
	}
	if signerEpoch != epoch || signerState != "open" {
		return zero, ErrSignerEpochClosed
	}
	leaves, e := delegationShareLeaves(d)
	if e != nil {
		return zero, e
	}
	for _, leaf := range leaves {
		if e = checkShareChainTx(ctx, tx, node, hub, leaf, true); e != nil {
			return zero, e
		}
	}
	heartbeat, deadline, e := issuerModeTx(ctx, tx, now)
	if e != nil {
		return zero, e
	}
	if now.Earliest.Before(d.ValidFrom) || !now.Latest.Before(d.ExpiresAt) || !now.Latest.Before(d.HardDeadline) || !now.Latest.Before(d.AutonomyDeadline) {
		return zero, ErrAuthorityExpired
	}
	// Initial credit must exist. This path cannot bootstrap an empty delegation.
	var initialAt string
	if e = tx.QueryRowContext(ctx, `SELECT created_at FROM issuer_issues WHERE delegation_id=? ORDER BY rowid DESC LIMIT 1`, delegation).Scan(&initialAt); e != nil {
		return zero, e
	}
	last, e := time.Parse(time.RFC3339Nano, initialAt)
	if e != nil {
		return zero, ErrAuthorizationDenied
	}
	var revision int64
	var prior IssuedAuthorization
	var priorAt string
	// Both names are fixed local migration identifiers, never wire input.
	table, interval, domain := "issuer_lease_refreshes", time.Minute, "runtime-lease-refresh"
	if heartbeatOnly {
		table, interval, domain = "issuer_runtime_heartbeats", 10*time.Second, "runtime-heartbeat"
	}
	e = tx.QueryRowContext(ctx, `SELECT revision,issued_at,payload,payload_sha256 FROM `+table+` WHERE delegation_id=? ORDER BY revision DESC LIMIT 1`, delegation).Scan(&revision, &priorAt, &prior.Payload, &prior.Digest)
	if e == nil {
		sum := sha256.Sum256(prior.Payload)
		at, err := time.Parse(time.RFC3339Nano, priorAt)
		if err != nil || !bytes.Equal(sum[:], prior.Digest) {
			return zero, ErrAuthorizationDenied
		}
		if at.After(last) {
			last = at
		}
		prior.Generation = d.PathGeneration
	} else if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	if now.Earliest.Before(last.Add(interval)) {
		return prior, tx.Commit()
	}
	expires := now.Latest.Add(300 * time.Second)
	hard := d.HardDeadline
	for _, end := range []time.Time{d.ExpiresAt, d.HardDeadline, d.AutonomyDeadline, deadline} {
		if expires.After(end) {
			expires = end
		}
		if hard.After(end) {
			hard = end
		}
	}
	if !expires.After(now.Latest) {
		return zero, ErrAuthorityExpired
	}
	// Revision is durable; a new session, minute boundary or process restart
	// cannot change an already-issued refresh's bytes or effective time.
	seed := sha256.Sum256([]byte(fmt.Sprintf("vortex/%s/v1/%s/%d", domain, delegation, revision+1)))
	id := seed[:16]
	id[6] = id[6]&15 | 0x50
	id[8] = id[8]&63 | 0x80
	requestID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	kinds := []string{"runtime_lease", "share_lease"}
	if heartbeatOnly {
		kinds = []string{}
	}
	payload, e := buildAuthorizationPayload(d, IssueRequest{ID: requestID}, now.Latest, expires, hard, heartbeat, deadline, kinds)
	if e != nil {
		return zero, e
	}
	update := IssuedAuthorization{Payload: payload}
	if e = attachEpochHandoffTx(ctx, tx, d, &update); e != nil {
		return zero, e
	}
	payload = update.Payload
	sum = sha256.Sum256(payload)
	if _, e = tx.ExecContext(ctx, `INSERT INTO `+table+`(delegation_id,revision,issue_id,issued_at,payload,payload_sha256) VALUES(?,?,?,?,?,?)`, delegation, revision+1, requestID, now.Latest.UTC().Format(time.RFC3339Nano), payload, sum[:]); e != nil {
		return zero, e
	}
	if e = tx.Commit(); e != nil {
		return zero, e
	}
	return IssuedAuthorization{Generation: d.PathGeneration, Payload: payload, Digest: sum[:]}, nil
}

// RefreshHubLeases scans every configured delegation for this authenticated
// incarnation, independent of a Node engine's last selected rule scope.
func (s *Store) RefreshHubLeases(ctx context.Context, hub, node, boot, after string, now IssuerTime) (BootstrapPage, error) {
	var page BootstrapPage
	if s.owner != "hub" || !validIssuerUUID(hub) || !validIssuerUUID(node) || !validIssuerUUID(boot) || after != "" && !validIssuerUUID(after) {
		return page, ErrAuthorizationDenied
	}
	if e := checkIssuerTime(now); e != nil {
		return page, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT delegation_id FROM issuer_delegations WHERE hub_id=? AND state='open' AND delegation_id>? AND json_extract(CAST(payload AS TEXT),'$.NodeID')=? AND json_extract(CAST(payload AS TEXT),'$.BootID')=? ORDER BY delegation_id LIMIT 32`, hub, after, node, boot)
	if e != nil {
		return page, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return page, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return page, e
	}
	page.Complete = len(ids) < 32
	for _, id := range ids {
		page.After = id
		heartbeat, e := s.RefreshHubRuntimeHeartbeat(ctx, hub, node, boot, id, now)
		if errors.Is(e, sql.ErrNoRows) || errors.Is(e, ErrAuthorityExpired) || errors.Is(e, ErrAuthorizationDenied) || errors.Is(e, ErrSignerEpochClosed) {
			continue
		}
		if e != nil {
			return BootstrapPage{}, e
		}
		if len(heartbeat.Payload) > 0 {
			page.Updates = append(page.Updates, heartbeat)
		}
		update, e := s.RefreshHubDelegationLeases(ctx, hub, node, boot, id, now)
		if errors.Is(e, sql.ErrNoRows) || errors.Is(e, ErrAuthorityExpired) || errors.Is(e, ErrAuthorizationDenied) || errors.Is(e, ErrSignerEpochClosed) {
			continue
		}
		if e != nil {
			return BootstrapPage{}, e
		}
		if len(update.Payload) > 0 {
			page.Updates = append(page.Updates, update)
		}
	}
	return page, nil
}

func (s *Store) migrateHubLeaseRefreshes() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS issuer_lease_refreshes (
 delegation_id TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),issue_id TEXT NOT NULL UNIQUE,
 issued_at TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(delegation_id,revision),FOREIGN KEY(delegation_id) REFERENCES issuer_delegations(delegation_id));
CREATE TRIGGER IF NOT EXISTS lease_refresh_no_update BEFORE UPDATE ON issuer_lease_refreshes BEGIN SELECT RAISE(ABORT,'immutable lease refresh'); END;
CREATE TRIGGER IF NOT EXISTS lease_refresh_no_delete BEFORE DELETE ON issuer_lease_refreshes BEGIN SELECT RAISE(ABORT,'immutable lease refresh'); END;
CREATE TRIGGER IF NOT EXISTS hub_journal_lease_refresh AFTER INSERT ON issuer_lease_refreshes BEGIN
 INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256) VALUES('runtime-lease',NEW.issue_id,NEW.payload,NEW.payload_sha256); END;
CREATE TABLE IF NOT EXISTS issuer_runtime_heartbeats (
 delegation_id TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),issue_id TEXT NOT NULL UNIQUE,
 issued_at TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(delegation_id,revision),FOREIGN KEY(delegation_id) REFERENCES issuer_delegations(delegation_id));
CREATE TRIGGER IF NOT EXISTS runtime_heartbeat_no_update BEFORE UPDATE ON issuer_runtime_heartbeats BEGIN SELECT RAISE(ABORT,'immutable runtime heartbeat'); END;
CREATE TRIGGER IF NOT EXISTS runtime_heartbeat_no_delete BEFORE DELETE ON issuer_runtime_heartbeats BEGIN SELECT RAISE(ABORT,'immutable runtime heartbeat'); END;
CREATE TRIGGER IF NOT EXISTS hub_journal_runtime_heartbeat AFTER INSERT ON issuer_runtime_heartbeats BEGIN
 INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256) VALUES('runtime-lease',NEW.issue_id,NEW.payload,NEW.payload_sha256); END;`); e != nil {
		return e
	}
	return tx.Commit()
}
