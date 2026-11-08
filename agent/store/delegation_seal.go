package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
	"vortex/contracts"
)

// SealHubDelegation cannot erase or return issued credit. It closes new issues,
// commits the entire issue inventory digest and journals the never-issued
// remainder in one FULL transaction. Old issue requests still replay old bytes.
func (s *Store) SealHubDelegation(ctx context.Context, hub, boot, id string) (contracts.DelegationSeal, error) {
	var zero contracts.DelegationSeal
	if s.owner != "hub" || !validIssuerUUID(hub) || !validIssuerUUID(boot) || !validIssuerUUID(id) {
		return zero, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	var prior []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM funded_delegation_seals WHERE delegation_id=?`, id).Scan(&prior)
	if e == nil {
		r, e := contracts.DecodeDelegationSeal(prior)
		if e != nil {
			return zero, e
		}
		if r.HubID != hub || r.HubBootID != boot {
			return zero, ErrAuthorizationDenied
		}
		return r, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	var raw, digest []byte
	var remaining, epoch string
	if e = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,remaining_bytes,signer_epoch FROM issuer_delegations WHERE delegation_id=?`, id).Scan(&raw, &digest, &remaining, &epoch); e != nil {
		return zero, e
	}
	sum := sha256.Sum256(raw)
	var d HubDelegation
	if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &d) != nil || d.HubID != hub || d.ID != id || d.SignerEpoch > math.MaxInt64 || epoch != strconv.FormatUint(d.SignerEpoch, 10) {
		return zero, ErrAuthorizationDenied
	}
	left, e := strconv.ParseInt(remaining, 10, 64)
	if e != nil || left < 0 || uint64(left) > d.RawBytes {
		return zero, ErrAuthorizationDenied
	}
	rows, e := tx.QueryContext(ctx, `SELECT issue_id,payload_sha256,amount_bytes FROM issuer_issues WHERE delegation_id=? ORDER BY issue_id`, id)
	if e != nil {
		return zero, e
	}
	inventory := []contracts.IssuedPartition{}
	for rows.Next() {
		var p contracts.IssuedPartition
		var hash []byte
		var amount string
		if e = rows.Scan(&p.ID, &hash, &amount); e != nil {
			rows.Close()
			return zero, e
		}
		p.Digest = hex.EncodeToString(hash)
		p.Raw, e = strconv.ParseInt(amount, 10, 64)
		if e != nil {
			rows.Close()
			return zero, e
		}
		inventory = append(inventory, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return zero, e
	}
	inventoryDigest, issued, e := contracts.PartitionDigest(inventory)
	if e != nil || uint64(issued) > d.RawBytes || uint64(left) != d.RawBytes-uint64(issued) {
		return zero, ErrAuthorizationDenied
	}
	sealHash := sha256.Sum256([]byte("vortex/delegation-seal/v1/" + id))
	sealHash[6] = sealHash[6]&0x0f | 0x50
	sealHash[8] = sealHash[8]&0x3f | 0x80
	sealID := hex.EncodeToString(sealHash[:16])
	sealID = sealID[:8] + "-" + sealID[8:12] + "-" + sealID[12:16] + "-" + sealID[16:20] + "-" + sealID[20:]
	r := contracts.DelegationSeal{Version: 1, ID: sealID, DelegationID: id, HubID: hub, HubBootID: boot, NodeID: d.NodeID, BootID: d.BootID, SignerEpoch: int64(d.SignerEpoch), Raw: int64(d.RawBytes), Issued: issued, Unissued: left, IssueCount: int64(len(inventory)), IssueDigest: inventoryDigest}
	if e = r.Validate(); e != nil {
		return zero, e
	}
	encoded, e := json.Marshal(r)
	if e != nil {
		return zero, e
	}
	receiptDigest := sha256.Sum256(encoded)
	if _, e = tx.ExecContext(ctx, `UPDATE issuer_delegations SET state='sealed' WHERE delegation_id=?`, id); e != nil {
		return zero, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO funded_delegation_seals(delegation_id,payload) VALUES(?,?)`, id, encoded); e != nil {
		return zero, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256) VALUES('delegation-seal',?,?,?)`, r.ID, encoded, receiptDigest[:]); e != nil {
		return zero, e
	}
	if e = tx.Commit(); e != nil {
		return zero, e
	}
	return r, nil
}

// SealExpiredHubDelegations scans one bounded page. Expiration here proves only
// that this Hub will issue no more, never that a Node stopped or debt vanished.
func (s *Store) SealExpiredHubDelegations(ctx context.Context, hub, boot, after string, now IssuerTime) (string, bool, error) {
	if e := checkIssuerTime(now); e != nil {
		return "", false, e
	}
	if now.Latest.Sub(now.Earliest) > 10*time.Second {
		return "", false, ErrAuthorityTimeUncertain
	}
	rows, e := s.db.QueryContext(ctx, `SELECT payload,state FROM issuer_delegations d WHERE hub_id=? AND delegation_id>? AND NOT EXISTS(SELECT 1 FROM funded_delegation_seals x WHERE x.delegation_id=d.delegation_id) ORDER BY delegation_id LIMIT 32`, hub, after)
	if e != nil {
		return "", false, e
	}
	type candidate struct {
		d     HubDelegation
		state string
	}
	var page []candidate
	for rows.Next() {
		var raw []byte
		var c candidate
		if e = rows.Scan(&raw, &c.state); e != nil {
			rows.Close()
			return "", false, e
		}
		if e = json.Unmarshal(raw, &c.d); e != nil {
			rows.Close()
			return "", false, e
		}
		page = append(page, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", false, e
	}
	for _, c := range page {
		after = c.d.ID
		if c.state != "closed" && now.Earliest.Before(c.d.ExpiresAt) && now.Earliest.Before(c.d.HardDeadline) && now.Earliest.Before(c.d.AutonomyDeadline) {
			continue
		}
		if _, e = s.SealHubDelegation(ctx, hub, boot, c.d.ID); e != nil {
			return "", false, e
		}
	}
	return after, len(page) < 32, nil
}
func (s *Store) migrateDelegationSeals() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS funded_delegation_seals(delegation_id TEXT PRIMARY KEY,payload BLOB NOT NULL);
CREATE TRIGGER IF NOT EXISTS delegation_seal_no_update BEFORE UPDATE ON funded_delegation_seals BEGIN SELECT RAISE(ABORT,'immutable seal'); END;
CREATE TRIGGER IF NOT EXISTS delegation_seal_no_delete BEFORE DELETE ON funded_delegation_seals BEGIN SELECT RAISE(ABORT,'immutable seal'); END;`); e != nil {
		return e
	}
	return tx.Commit()
}
