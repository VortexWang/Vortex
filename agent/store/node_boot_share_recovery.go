package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"vortex/contracts"
)

// RetireNodeBootShares is a local FULL consumer for an independently negotiated,
// pinned direct-backend command. Its caller supplies the actual current boot.
// This primitive alone is not production transport or PostgreSQL approval.
// Old command receipts/activation holds/credit debt remain unchanged. A new
// boot starts at the original per-kind floor, with no active capacity, and can
// become active only through the next explicit CAS on that exact incarnation.
func (s *Store) RetireNodeBootShares(ctx context.Context, node, boot, backend string, raw []byte, now IssuerTime) (contracts.NodeBootRecoveryReceipt, error) {
	var zero contracts.NodeBootRecoveryReceipt
	c, e := contracts.DecodeNodeBootRecovery(raw)
	if e != nil {
		return zero, e
	}
	if s.owner != "agent" || c.NodeID != node || c.ToBootID != boot || c.BackendID != backend {
		return zero, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	var prior, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT payload,receipt FROM node_boot_share_recoveries WHERE recovery_id=?`, c.ID).Scan(&prior, &receipt)
	if e == nil {
		expected := nodeBootRetirementReceipt(c, raw)
		canonical, _ := json.Marshal(expected)
		if !bytes.Equal(prior, raw) || !bytes.Equal(receipt, canonical) {
			return zero, ErrAuthorizationUpdateConflict
		}
		return expected, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	if e = checkIssuerTime(now); e != nil {
		return zero, e
	}
	if now.Latest.Sub(now.Earliest) > 10*time.Second {
		return zero, ErrAuthorityTimeUncertain
	}
	if now.Earliest.Before(c.SafeAfter) || now.Earliest.Before(c.IssuedAt) || !now.Latest.Before(c.ExpiresAt) {
		return zero, ErrAuthorityExpired
	}
	// Independently retain any later old-boot hard boundary already known to
	// this SQLite store, including an issue not yet acknowledged by PostgreSQL.
	rows, e := tx.QueryContext(ctx, `SELECT hard_deadline FROM authorization_records WHERE node_id=? AND boot_id=?`, node, c.FromBootID)
	if e != nil {
		return zero, e
	}
	for rows.Next() {
		var text string
		if e = rows.Scan(&text); e != nil {
			rows.Close()
			return zero, e
		}
		end, parseErr := time.Parse(time.RFC3339Nano, text)
		if parseErr != nil || c.SafeAfter.Before(end) {
			rows.Close()
			return zero, ErrAuthorityExpired
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return zero, e
	}
	// An omitted or different kind/head can never be reset by this pair. Any
	// higher generation, changed original command, wrong old boot or floor denies
	// the entire transaction, including on an existing partially populated store.
	for _, p := range c.Previous {
		var tenant string
		e = tx.QueryRowContext(ctx, `SELECT tenant_id FROM node_share_bindings WHERE node_id=? AND rule_id=?`, node, p.RuleID).Scan(&tenant)
		if e == nil && tenant != c.TenantID {
			return zero, ErrAuthorizationDenied
		}
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO node_share_bindings(node_id,rule_id,tenant_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`, node, p.RuleID, c.TenantID); e != nil {
			return zero, e
		}
		var oldBoot string
		var generation, floor int64
		var original []byte
		e = tx.QueryRowContext(ctx, `SELECT boot_id,generation,fencing_generation,payload FROM node_share_heads WHERE tenant_id=? AND node_id=? AND kind=?`, c.TenantID, node, p.Kind).Scan(&oldBoot, &generation, &floor, &original)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
		encoded, e2 := json.Marshal(p)
		if e2 != nil {
			return zero, e2
		}
		if e == nil && (oldBoot != c.FromBootID || generation != p.Generation || floor >= c.MinimumFencing || !bytes.Equal(original, encoded)) {
			return zero, ErrAuthorizationGenerationStale
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO node_share_heads(tenant_id,node_id,kind,boot_id,generation,fencing_generation,amount,burst_bytes,payload) VALUES(?,?,?,?,?,?,0,0,?) ON CONFLICT(tenant_id,node_id,kind) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,fencing_generation=excluded.fencing_generation,amount=0,burst_bytes=0,payload=excluded.payload`, c.TenantID, node, p.Kind, boot, p.Generation, c.MinimumFencing, encoded); e != nil {
			return zero, e
		}
	}
	zero = nodeBootRetirementReceipt(c, raw)
	encoded, e := json.Marshal(zero)
	if e != nil {
		return contracts.NodeBootRecoveryReceipt{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO node_boot_share_recoveries(recovery_id,payload,receipt) VALUES(?,?,?)`, c.ID, raw, encoded); e != nil {
		return contracts.NodeBootRecoveryReceipt{}, e
	}
	if e = tx.Commit(); e != nil {
		return contracts.NodeBootRecoveryReceipt{}, e
	}
	return zero, nil
}

func nodeBootRetirementReceipt(c contracts.NodeBootRecovery, raw []byte) contracts.NodeBootRecoveryReceipt {
	r := contracts.NodeBootRecoveryReceipt{Version: 1, ID: c.ID, NodeID: c.NodeID, BootID: c.ToBootID, CommandSHA256: contracts.SharePayloadDigest(raw), MinimumFencing: c.MinimumFencing, Retired: []contracts.RetiredNodeShareHead{}}
	for _, p := range c.Previous {
		r.Retired = append(r.Retired, contracts.RetiredNodeShareHead{Kind: p.Kind, Generation: p.Generation})
	}
	return r
}

func (s *Store) migrateNodeBootShareRecovery() error {
	_, e := s.db.Exec(`CREATE TABLE IF NOT EXISTS node_boot_share_recoveries(recovery_id TEXT PRIMARY KEY NOT NULL,payload BLOB NOT NULL,receipt BLOB NOT NULL);
CREATE TRIGGER IF NOT EXISTS node_boot_recovery_no_update BEFORE UPDATE ON node_boot_share_recoveries BEGIN SELECT RAISE(ABORT,'immutable node boot retirement'); END;
CREATE TRIGGER IF NOT EXISTS node_boot_recovery_no_delete BEFORE DELETE ON node_boot_share_recoveries BEGIN SELECT RAISE(ABORT,'immutable node boot retirement'); END;`)
	return e
}
