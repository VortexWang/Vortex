package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

// HubDelegation is installed only by the authenticated backend adapter. It is
// an immutable allocation to an already configured node/rule/share, in raw
// bytes. Billing-unit conversion and backend source allocation precede this
// boundary. No node request can install or change it.
type HubDelegation struct {
	ID, HubID, SourceID, RuntimeSourceID, ShareSourceID     string
	NodeID, BootID, RuleID, BillingSnapshotID               string
	SignerEpoch, RuleRevision, PathGeneration, FencingFloor uint64
	RawBytes, RateBPS, Connections                          uint64
	ValidFrom, ExpiresAt, HardDeadline                      time.Time
	BackendHeartbeat, AutonomyDeadline                      time.Time
}

type IssuerTime struct {
	Earliest, Latest time.Time
	Trusted          bool
}

// IssueRequest names only a pre-existing delegation and the receiving boot.
// It cannot create rules, ports, DNS entries, registrations or share transfers.
type IssueRequest struct {
	ID, HubID, DelegationID, NodeID, BootID string
	RawBytes                                uint64
	TTL                                     time.Duration
}

type IssuedAuthorization struct {
	Generation uint64
	Payload    []byte
	Digest     []byte
}

func validIssuerUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func checkIssuerTime(now IssuerTime) error {
	if !now.Trusted || now.Earliest.IsZero() || now.Latest.Before(now.Earliest) {
		return ErrAuthorityTimeUncertain
	}
	return nil
}

// InstallHubDelegation commits an authenticated backend allocation and its
// heartbeat together. Replaying the same allocation never refills its balance.
// A higher epoch requires explicit handoff; a closed epoch cannot be reopened.
func (s *Store) InstallHubDelegation(ctx context.Context, d HubDelegation, now IssuerTime) error {
	if s.owner != "hub" {
		return ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return err
	}
	for _, id := range []string{d.ID, d.HubID, d.SourceID, d.RuntimeSourceID, d.ShareSourceID, d.NodeID, d.BootID, d.BillingSnapshotID} {
		if !validIssuerUUID(id) {
			return ErrAuthorizationDenied
		}
	}
	if d.RuleID == "" || len(d.RuleID) > 128 || d.SignerEpoch == 0 || d.RuleRevision == 0 || d.PathGeneration == 0 || d.FencingFloor == 0 || d.RawBytes == 0 || d.RawBytes > math.MaxInt64 || d.RateBPS == 0 || d.RateBPS > 1_000_000_000_000 || d.Connections == 0 || d.Connections > 1_000_000 || d.ValidFrom.IsZero() || !d.ExpiresAt.After(d.ValidFrom) || d.ExpiresAt.After(d.HardDeadline) || d.HardDeadline.After(d.AutonomyDeadline) || d.BackendHeartbeat.IsZero() || !d.AutonomyDeadline.After(d.BackendHeartbeat) || d.AutonomyDeadline.After(d.BackendHeartbeat.Add(12*time.Hour)) {
		return ErrAuthorizationDenied
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&previous)
	if err == nil {
		if !bytes.Equal(previous, raw) {
			return ErrAuthorizationUpdateConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if now.Earliest.Before(d.BackendHeartbeat) || !now.Latest.Before(d.BackendHeartbeat.Add(BackendLossGrace)) || now.Earliest.Before(d.ValidFrom) || !now.Latest.Before(d.ExpiresAt) {
		return ErrNewAuthorityInAutonomy
	}
	if err := persistAuthorizationHeartbeat(ctx, tx, d.BackendHeartbeat, d.AutonomyDeadline); err != nil {
		return err
	}
	var epoch, state string
	err = tx.QueryRowContext(ctx, `SELECT signer_epoch,state FROM issuer_epochs WHERE hub_id=?`, d.HubID).Scan(&epoch, &state)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO issuer_epochs(hub_id,signer_epoch,state) VALUES(?,?,'open')`, d.HubID, strconv.FormatUint(d.SignerEpoch, 10))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if epoch != strconv.FormatUint(d.SignerEpoch, 10) {
		return ErrAuthorityHandoffRequired
	} else if state != "open" {
		return ErrSignerEpochClosed
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO issuer_delegations(delegation_id,hub_id,source_id,signer_epoch,payload,payload_sha256,remaining_bytes,state) VALUES(?,?,?,?,?,?,?,'open')`, d.ID, d.HubID, d.SourceID, strconv.FormatUint(d.SignerEpoch, 10), raw, digest[:], strconv.FormatUint(d.RawBytes, 10))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// issuerModeTx evaluates the fixed backend boundary in the same transaction
// that spends the delegation. General online signing closes at loss; only
// installed, node-bound delegations can still issue in restricted autonomy.
func issuerModeTx(ctx context.Context, tx *sql.Tx, now IssuerTime) (time.Time, time.Time, error) {
	if err := checkIssuerTime(now); err != nil {
		return time.Time{}, time.Time{}, err
	}
	heartbeatText, err := stateValue(ctx, tx, "backend_heartbeat")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, heartbeatText)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	deadline := deadlineFromTx(ctx, tx)
	if deadline.IsZero() || now.Earliest.Before(heartbeat) {
		return time.Time{}, time.Time{}, ErrAuthorityTimeUncertain
	}
	mode, err := stateValue(ctx, tx, "authority_mode")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if mode != AutonomyOnline && mode != AutonomyOffline {
		return time.Time{}, time.Time{}, ErrAuthorityExpired
	}
	if !now.Latest.Before(deadline) {
		return time.Time{}, time.Time{}, ErrAuthorityExpired
	}
	if mode == AutonomyOnline && !now.Latest.Before(heartbeat.Add(BackendLossGrace)) {
		if err := putState(ctx, tx, "authority_mode", AutonomyOffline, now.Latest); err != nil {
			return time.Time{}, time.Time{}, err
		}
		if err := putState(ctx, tx, "signer_epoch_open", "false", now.Latest); err != nil {
			return time.Time{}, time.Time{}, err
		}
		if err := writeAutonomyTransition(ctx, tx, AutonomyOnline, AutonomyOffline, heartbeat.Add(BackendLossGrace), heartbeat, deadline); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return heartbeat, deadline, nil
}

// IssueHubAuthorization reserves delegated bytes and stores the exact outgoing
// payload in one FULL transaction. Lost responses only replay the same payload;
// expiry, disconnection and process restart do not return any reserved bytes.
func (s *Store) IssueHubAuthorization(ctx context.Context, r IssueRequest, now IssuerTime) (IssuedAuthorization, error) {
	var result IssuedAuthorization
	if s.owner != "hub" {
		return result, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return result, err
	}
	if !validIssuerUUID(r.ID) || r.RawBytes == 0 || r.RawBytes > math.MaxInt64 || r.TTL <= 0 || r.TTL > 300*time.Second {
		return result, ErrAuthorizationDenied
	}
	request, err := json.Marshal(r)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var priorRequest []byte
	err = tx.QueryRowContext(ctx, `SELECT request,payload,payload_sha256 FROM issuer_issues WHERE issue_id=?`, r.ID).Scan(&priorRequest, &result.Payload, &result.Digest)
	if err == nil {
		if !bytes.Equal(priorRequest, request) {
			return IssuedAuthorization{}, ErrAuthorizationUpdateConflict
		}
		digest := sha256.Sum256(result.Payload)
		if !bytes.Equal(digest[:], result.Digest) {
			return IssuedAuthorization{}, ErrAuthorizationUpdateConflict
		}
		var envelope struct {
			PathGeneration uint64 `json:"path_generation"`
		}
		if err := json.Unmarshal(result.Payload, &envelope); err != nil {
			return IssuedAuthorization{}, err
		}
		result.Generation = envelope.PathGeneration
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	var raw, digest []byte
	var remainingText, state, ownerEpoch string
	err = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,remaining_bytes,state,signer_epoch FROM issuer_delegations WHERE delegation_id=?`, r.DelegationID).Scan(&raw, &digest, &remainingText, &state, &ownerEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNewAuthorityInAutonomy
	}
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	if !bytes.Equal(sum[:], digest) {
		return result, ErrAuthorizationDenied
	}
	var d HubDelegation
	if err := json.Unmarshal(raw, &d); err != nil {
		return result, err
	}
	if state != "open" || d.HubID != r.HubID || d.NodeID != r.NodeID || d.BootID != r.BootID {
		return result, ErrAuthorizationDenied
	}
	var epoch, epochState string
	if err := tx.QueryRowContext(ctx, `SELECT signer_epoch,state FROM issuer_epochs WHERE hub_id=?`, d.HubID).Scan(&epoch, &epochState); err != nil {
		return result, err
	}
	if epoch != ownerEpoch || epochState != "open" {
		return result, ErrSignerEpochClosed
	}
	d.SignerEpoch, err = strconv.ParseUint(ownerEpoch, 10, 64)
	if err != nil || d.SignerEpoch == 0 {
		return result, ErrAuthorizationDenied
	}
	heartbeat, deadline, err := issuerModeTx(ctx, tx, now)
	if err != nil {
		return result, err
	}
	if now.Earliest.Before(d.ValidFrom) || !now.Latest.Before(d.ExpiresAt) || !now.Latest.Before(d.HardDeadline) {
		return result, ErrAuthorityExpired
	}
	remaining, err := strconv.ParseUint(remainingText, 10, 64)
	if err != nil {
		return result, err
	}
	if r.RawBytes > remaining {
		return result, ErrAuthorizationDenied
	}
	expires := now.Latest.Add(r.TTL)
	for _, limit := range []time.Time{d.ExpiresAt, d.HardDeadline, deadline, d.AutonomyDeadline} {
		if expires.After(limit) {
			expires = limit
		}
	}
	hard := d.HardDeadline
	for _, limit := range []time.Time{deadline, d.AutonomyDeadline} {
		if hard.After(limit) {
			hard = limit
		}
	}
	if !expires.After(now.Latest) {
		return result, ErrAuthorityExpired
	}
	result.Payload, err = buildIssuedPayload(d, r, now.Latest, expires, hard, heartbeat, deadline)
	if err != nil {
		return result, err
	}
	result.Generation = d.PathGeneration
	issuedHash := sha256.Sum256(result.Payload)
	result.Digest = issuedHash[:]
	if _, err := tx.ExecContext(ctx, `UPDATE issuer_delegations SET remaining_bytes=? WHERE delegation_id=?`, strconv.FormatUint(remaining-r.RawBytes, 10), d.ID); err != nil {
		return IssuedAuthorization{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO issuer_issues(issue_id,delegation_id,request,payload,payload_sha256,amount_bytes,created_at) VALUES(?,?,?,?,?,?,?)`, r.ID, d.ID, request, result.Payload, result.Digest, strconv.FormatUint(r.RawBytes, 10), now.Latest.UTC().Format(time.RFC3339Nano)); err != nil {
		return IssuedAuthorization{}, err
	}
	if err := tx.Commit(); err != nil {
		return IssuedAuthorization{}, err
	}
	return result, nil
}

func buildIssuedPayload(d HubDelegation, r IssueRequest, issued, expires, hard, heartbeat, deadline time.Time) ([]byte, error) {
	newID := func() (string, error) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return "", err
		}
		id[6] = (id[6] & 15) | 64
		id[8] = (id[8] & 63) | 128
		s := hex.EncodeToString(id[:])
		return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
	}
	entries := make([]map[string]any, 0, 3)
	for _, kind := range []string{"runtime_lease", "share_lease", "credit_grant"} {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		source := d.SourceID
		if kind == "runtime_lease" {
			source = d.RuntimeSourceID
		}
		if kind == "share_lease" {
			source = d.ShareSourceID
		}
		a := map[string]any{"authorization_id": id, "parent_authorization_id": d.ID, "source_id": source, "kind": kind, "rule_id": d.RuleID, "rule_revision": d.RuleRevision, "path_generation": d.PathGeneration, "issued_at": issued.UTC().Format(time.RFC3339Nano), "not_before": issued.UTC().Format(time.RFC3339Nano), "expires_at": expires.UTC().Format(time.RFC3339Nano), "hard_deadline": hard.UTC().Format(time.RFC3339Nano)}
		if kind == "share_lease" {
			a["rate_bps"] = d.RateBPS
			a["connections"] = d.Connections
		}
		if kind == "credit_grant" {
			a["billing_snapshot_id"] = d.BillingSnapshotID
			a["amount_bytes"] = r.RawBytes
		}
		entries = append(entries, a)
	}
	return json.Marshal(map[string]any{"schema_version": "1.0.0", "update_id": r.ID, "node_id": d.NodeID, "boot_id": d.BootID, "signer_epoch": d.SignerEpoch, "path_generation": d.PathGeneration, "fencing_generation_floor": d.FencingFloor, "issued_at": issued.UTC().Format(time.RFC3339Nano), "backend_heartbeat_at": heartbeat.UTC().Format(time.RFC3339Nano), "autonomy_deadline": deadline.UTC().Format(time.RFC3339Nano), "authorizations": entries, "revocations": []string{}, "restrictions": map[string]bool{"existing_rules_only": true, "existing_shares_only": true, "existing_delegated_balances_only": true, "new_rules": false, "new_ports": false, "dns_changes": false, "registrations": false, "cross_node_rebalance": false}})
}

// CloseHubSigner is irreversible here. Handoff and remainder transfer require
// the subsequent recovery transaction; installing a newer epoch cannot bypass it.
func (s *Store) CloseHubSigner(ctx context.Context, hubID string, epoch uint64) error {
	if s.owner != "hub" {
		return ErrAuthorizationDenied
	}
	result, err := s.db.ExecContext(ctx, `UPDATE issuer_epochs SET state='closed' WHERE hub_id=? AND signer_epoch=?`, hubID, strconv.FormatUint(epoch, 10))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSignerEpochClosed
	}
	return nil
}

type DelegationBalance struct{ Initial, Remaining, Issued uint64 }

func (s *Store) AuditHubDelegation(ctx context.Context, id string) (DelegationBalance, error) {
	var result DelegationBalance
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var raw []byte
	var remaining string
	if err := tx.QueryRowContext(ctx, `SELECT payload,remaining_bytes FROM issuer_delegations WHERE delegation_id=?`, id).Scan(&raw, &remaining); err != nil {
		return result, err
	}
	var d HubDelegation
	if err := json.Unmarshal(raw, &d); err != nil {
		return result, err
	}
	result.Initial = d.RawBytes
	result.Remaining, err = strconv.ParseUint(remaining, 10, 64)
	if err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT amount_bytes FROM issuer_issues WHERE delegation_id=?`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var amount string
		if err := rows.Scan(&amount); err != nil {
			rows.Close()
			return result, err
		}
		n, err := strconv.ParseUint(amount, 10, 64)
		if err != nil {
			rows.Close()
			return result, err
		}
		if n > math.MaxUint64-result.Issued {
			rows.Close()
			return result, ErrAuthorizationDenied
		}
		result.Issued += n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if result.Remaining > result.Initial || result.Issued != result.Initial-result.Remaining {
		return result, fmt.Errorf("store: delegated source conservation violated")
	}
	return result, tx.Commit()
}
