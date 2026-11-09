package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
)

// GrantSettlementProof is a local immutable ledger snapshot, not authenticated
// wire evidence or issuer credit. Unknown remains a liability; only Unused may
// be considered by a future authenticated, reconciled return protocol.
type GrantSettlementProof struct {
	Scope                          DemandScope
	GrantID                        string
	Initial, Used, Unknown, Unused uint64
}

func (s *Store) PrepareGrantSettlementProof(ctx context.Context, scope DemandScope, grantID string) (GrantSettlementProof, []byte, error) {
	var empty GrantSettlementProof
	if s.owner != "agent" {
		return empty, nil, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, nil, err
	}
	defer tx.Rollback()
	key, err := json.Marshal(scope)
	if err != nil {
		return empty, nil, err
	}
	var sealedScope []byte
	if err := tx.QueryRowContext(ctx, `SELECT scope FROM sealed_grants WHERE node_id=? AND grant_id=?`, scope.NodeID, grantID).Scan(&sealedScope); err != nil {
		return empty, nil, err
	}
	if !bytes.Equal(key, sealedScope) {
		return empty, nil, ErrAuthorizationUpdateConflict
	}
	record, state, err := readAuthorizationTx(ctx, tx, scope.NodeID, grantID)
	if err != nil {
		return empty, nil, err
	}
	if state == "active" || demandScopeFor(scope.NodeID, record) != scope {
		return empty, nil, ErrAuthorizationDenied
	}
	// Read and check the current ledger even on replay. A changed ledger cannot
	// silently reuse an old snapshot as proof of a different unused amount.
	balance, err := auditAuthorizationBalanceTx(ctx, tx, scope.NodeID, grantID)
	if err != nil {
		return empty, nil, err
	}
	if balance.Reserved != 0 || balance.Initial != record.AmountBytes {
		return empty, nil, ErrAuthorizationDenied
	}
	proof := GrantSettlementProof{Scope: scope, GrantID: grantID, Initial: balance.Initial, Used: balance.Committed, Unknown: balance.Unknown, Unused: balance.Remaining}
	payload, err := json.Marshal(proof)
	if err != nil {
		return empty, nil, err
	}
	digest := sha256.Sum256(payload)
	var previous, hash []byte
	err = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM grant_settlement_proofs WHERE node_id=? AND grant_id=?`, scope.NodeID, grantID).Scan(&previous, &hash)
	if err == nil {
		if !bytes.Equal(previous, payload) || !bytes.Equal(hash, digest[:]) {
			return empty, nil, ErrAuthorizationUpdateConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO grant_settlement_proofs(node_id,grant_id,payload,payload_sha256) VALUES(?,?,?,?)`, scope.NodeID, grantID, payload, digest[:]); err != nil {
			return empty, nil, err
		}
	} else {
		return empty, nil, err
	}
	if err := tx.Commit(); err != nil {
		return empty, nil, err
	}
	return proof, digest[:], nil
}
