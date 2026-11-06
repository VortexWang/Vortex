package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SealIdleGrant is an explicit local drain operation, not an unused-credit
// proof or a refund. It requires no live session referencing this grant,
// including original session anchors. Outstanding batches can still settle;
// their unknown liabilities never disappear. Timed idle policy is separate.
// Once committed, no new reservation can use the grant, including after reboot.
func (s *Store) SealIdleGrant(ctx context.Context, scope DemandScope, grantID string) error {
	if s.owner != "agent" || grantID == "" {
		return ErrAuthorizationDenied
	}
	key, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sealGrantTx(ctx, tx, scope, grantID, key); err != nil {
		return err
	}
	return tx.Commit()
}

func sealGrantTx(ctx context.Context, tx *sql.Tx, scope DemandScope, grantID string, key []byte) error {
	var previous []byte
	err := tx.QueryRowContext(ctx, `SELECT scope FROM sealed_grants WHERE node_id=? AND grant_id=?`, scope.NodeID, grantID).Scan(&previous)
	if err == nil {
		if !bytes.Equal(previous, key) {
			return ErrAuthorizationUpdateConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	record, state, err := readAuthorizationTx(ctx, tx, scope.NodeID, grantID)
	if err != nil {
		return err
	}
	if record.Kind != "credit_grant" || demandScopeFor(scope.NodeID, record) != scope || state != "active" {
		return ErrAuthorizationDenied
	}
	var sessions int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND state='open' AND (grant_id=? OR initial_grant_id=?)`, scope.NodeID, grantID, grantID).Scan(&sessions); err != nil {
		return err
	}
	if sessions != 0 {
		return ErrAuthorizationDenied
	}
	// Seal and admission both acquire SQLite's write transaction: either an
	// admission wins and this rejects, or sealing wins and admission rejects.
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET state='settlement_only' WHERE node_id=? AND authorization_id=? AND state='active'`, scope.NodeID, grantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sealed_grants(node_id,grant_id,scope) VALUES(?,?,?)`, scope.NodeID, grantID, key); err != nil {
		return err
	}
	return nil
}
