package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (s *Store) migrateDemandDenialState() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var definition string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='authorization_demands'`).Scan(&definition); err != nil {
		return err
	}
	if !strings.Contains(definition, "'denied'") {
		for _, q := range []string{
			`CREATE TABLE authorization_demands_v14(request_id TEXT PRIMARY KEY NOT NULL,scope BLOB NOT NULL,request BLOB NOT NULL,state TEXT NOT NULL CHECK(state IN ('pending','applied','denied')),grant_id TEXT NOT NULL DEFAULT '',denial_reason TEXT NOT NULL DEFAULT '')`,
			`INSERT INTO authorization_demands_v14(request_id,scope,request,state,grant_id) SELECT request_id,scope,request,state,grant_id FROM authorization_demands`,
			`DROP TABLE authorization_demands`,
			`ALTER TABLE authorization_demands_v14 RENAME TO authorization_demands`,
			`CREATE UNIQUE INDEX one_pending_authorization_demand ON authorization_demands(scope) WHERE state='pending'`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// PendingDemands pages only the active boot's requests for the authenticated
// Hub. Old boots remain durable and unresolved, never dispatched as a new boot.
func (s *Store) PendingDemands(ctx context.Context, node, boot, hub, after string) ([]IssueRequest, error) {
	if s.owner != "agent" || !validIssuerUUID(node) || !validIssuerUUID(boot) || !validIssuerUUID(hub) {
		return nil, ErrAuthorizationDenied
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request FROM authorization_demands WHERE state='pending' AND request_id>? AND json_extract(request,'$.NodeID')=? AND json_extract(request,'$.BootID')=? AND json_extract(request,'$.HubID')=? ORDER BY request_id LIMIT 32`, after, node, boot, hub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]IssueRequest, 0, 32)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r IssueRequest
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// DenyDemand applies only a definitive response received from the pinned
// authenticated Hub. It neither touches installed grants nor refunds liabilities.
func (s *Store) DenyDemand(ctx context.Context, node, boot, hub, id, reason string) error {
	if s.owner != "agent" || !validIssuerUUID(id) || (reason != "invalid_scope" && reason != "insufficient_funds" && reason != "expired") {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var state, prior string
	if err := tx.QueryRowContext(ctx, `SELECT scope,state,denial_reason FROM authorization_demands WHERE request_id=?`, id).Scan(&raw, &state, &prior); err != nil {
		return err
	}
	var scope DemandScope
	if err := json.Unmarshal(raw, &scope); err != nil {
		return err
	}
	if scope.NodeID != node || scope.BootID != boot || scope.HubID != hub {
		return ErrAuthorizationDenied
	}
	if state == "applied" || state == "denied" && prior != reason {
		return ErrAuthorizationUpdateConflict
	}
	if state == "denied" {
		return tx.Commit()
	}
	if err := recordDemandRetryTx(ctx, tx, raw, reason, time.Now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_demands SET state='denied',denial_reason=? WHERE request_id=? AND state='pending'`, reason, id); err != nil {
		return err
	}
	return tx.Commit()
}
