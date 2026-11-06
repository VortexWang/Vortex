package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type DemandIdleObservation struct {
	Idle            bool
	MinimumDuration time.Duration
}

// ObserveDemandIdle records a conservative lower bound on quiescence. Activity
// triggers invalidate all observations for this node/rule (even another path),
// preventing short-lived work between scans from being mistaken for inactivity.
// It is a local scheduling observation, never a refund or stop-execution proof.
func (s *Store) ObserveDemandIdle(ctx context.Context, scope DemandScope, now IssuerTime) (DemandIdleObservation, error) {
	var empty DemandIdleObservation
	if s.owner != "agent" || scope.NodeID == "" || scope.BootID == "" || scope.RuleID == "" {
		return empty, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return empty, err
	}
	key, err := json.Marshal(scope)
	if err != nil {
		return empty, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var active int
	// Count old boots too. Their unproven session/hold cannot establish idle.
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions s JOIN authorization_records a ON a.node_id=s.node_id AND a.authorization_id=s.grant_id WHERE s.node_id=? AND a.rule_id=? AND s.state='open'`, scope.NodeID, scope.RuleID).Scan(&active); err != nil {
		return empty, err
	}
	var holds int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_usage_holds h JOIN authorization_records a ON a.node_id=h.node_id AND a.authorization_id=h.authorization_id WHERE h.node_id=? AND a.rule_id=? AND h.state='reserved'`, scope.NodeID, scope.RuleID).Scan(&holds); err != nil {
		return empty, err
	}
	var demands int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_demands WHERE state='pending' AND json_extract(scope,'$.NodeID')=? AND json_extract(scope,'$.RuleID')=?`, scope.NodeID, scope.RuleID).Scan(&demands); err != nil {
		return empty, err
	}
	if active != 0 || holds != 0 || demands != 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM demand_idle_observations WHERE scope=?`, key); err != nil {
			return empty, err
		}
		return empty, tx.Commit()
	}
	var sinceText, lastText string
	err = tx.QueryRowContext(ctx, `SELECT since_latest,observed_latest FROM demand_idle_observations WHERE scope=?`, key).Scan(&sinceText, &lastText)
	since := now.Latest
	if err == nil {
		var parseErr error
		since, parseErr = time.Parse(time.RFC3339Nano, sinceText)
		if parseErr != nil {
			return empty, parseErr
		}
		last, parseErr := time.Parse(time.RFC3339Nano, lastText)
		if parseErr != nil {
			return empty, parseErr
		}
		if now.Latest.Before(last) || now.Latest.Before(since) {
			// Forget the interval durably. A later clock recovery must start over.
			if _, err := tx.ExecContext(ctx, `DELETE FROM demand_idle_observations WHERE scope=?`, key); err != nil {
				return empty, err
			}
			if err := tx.Commit(); err != nil {
				return empty, err
			}
			return empty, ErrAuthorityTimeUncertain
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO demand_idle_observations(scope,node_id,rule_id,since_latest,observed_latest) VALUES(?,?,?,?,?) ON CONFLICT(scope) DO UPDATE SET observed_latest=excluded.observed_latest`, key, scope.NodeID, scope.RuleID, since.UTC().Format(time.RFC3339Nano), now.Latest.UTC().Format(time.RFC3339Nano)); err != nil {
		return empty, err
	}
	duration := now.Earliest.Sub(since)
	if duration < 0 {
		duration = 0
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return DemandIdleObservation{Idle: true, MinimumDuration: duration}, nil
}
