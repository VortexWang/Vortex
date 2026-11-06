package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Local additive migration. Historical handoffs lack a completion witness, so
// capture all retained ledger rows conservatively; never infer online from ACKs
// or a reconnect alone. Reopening must not move an already captured barrier.
func (s *Store) migrateRecoveryMode() error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hub_recoveries(handoff_id TEXT PRIMARY KEY REFERENCES hub_handoffs(handoff_id),target_sequence INTEGER NOT NULL CHECK(target_sequence>0),completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)))`); err != nil {
		return err
	}
	var version string
	err = tx.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key='recovery_mode_version'`).Scan(&version)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if s.owner == "hub" {
		var id string
		err = tx.QueryRowContext(ctx, `SELECT handoff_id FROM hub_handoffs ORDER BY rowid DESC LIMIT 1`).Scan(&id)
		if err == nil {
			if _, err = tx.ExecContext(ctx, `INSERT INTO hub_recoveries(handoff_id,target_sequence) SELECT ?,MAX(sequence) FROM hub_ledger`, id); err != nil {
				return err
			}
			if err = putState(ctx, tx, "recovery_handoff_id", id, time.Now().UTC()); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE agent_state SET value='recovering' WHERE key='authority_mode' AND value='online'`); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_meta(key,value) VALUES('recovery_mode_version','1')`); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteRecovery is called only after authenticated, hash-bound ledger ACKs.
// The finite barrier includes the handoff record itself. Later tail is reported
// independently and cannot starve recovery or be implicitly acknowledged here.
func (s *Store) CompleteRecovery(ctx context.Context, now IssuerTime) (bool, error) {
	if s.owner != "hub" {
		return false, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	mode, err := stateValue(ctx, tx, "authority_mode")
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if mode != AutonomyRecovering {
		return false, tx.Commit()
	}
	if err := checkIssuerTime(now); err != nil {
		return false, err
	}
	open, err := stateValue(ctx, tx, "signer_epoch_open")
	if err != nil {
		return false, err
	}
	if open != "true" {
		return false, ErrSignerEpochClosed
	}
	var target int64
	var epoch, state string
	err = tx.QueryRowContext(ctx, `SELECT r.target_sequence,e.signer_epoch,e.state FROM hub_recoveries r JOIN hub_handoffs h ON h.handoff_id=r.handoff_id JOIN issuer_epochs e ON e.hub_id=h.hub_id AND e.signer_epoch=h.to_epoch WHERE r.handoff_id=(SELECT value FROM agent_state WHERE key='recovery_handoff_id') AND r.completed=0`).Scan(&target, &epoch, &state)
	if err != nil {
		return false, err
	}
	if state != "open" {
		return false, ErrSignerEpochClosed
	}
	hb, err := stateValue(ctx, tx, "backend_heartbeat")
	if err != nil {
		return false, err
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, hb)
	if err != nil {
		return false, err
	}
	if now.Earliest.Before(heartbeat) {
		return false, ErrAuthorityTimeUncertain
	}
	deadline := deadlineFromTx(ctx, tx)
	if deadline.IsZero() || !now.Latest.Before(deadline) {
		return false, ErrAuthorityExpired
	}
	if !now.Latest.Before(heartbeat.Add(BackendLossGrace)) {
		return false, ErrAuthorityHandoffRequired
	}
	var checkpoint, holes int64
	if err = tx.QueryRowContext(ctx, `SELECT CAST(COALESCE((SELECT value FROM agent_state WHERE key='ledger_checkpoint'),'0') AS INTEGER),(SELECT COUNT(*) FROM hub_ledger WHERE sequence<=? AND confirmed=0)`, target).Scan(&checkpoint, &holes); err != nil {
		return false, err
	}
	if checkpoint < target || holes != 0 {
		return false, tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hub_recoveries SET completed=1 WHERE handoff_id=(SELECT value FROM agent_state WHERE key='recovery_handoff_id')`); err != nil {
		return false, err
	}
	if err = putState(ctx, tx, "authority_mode", AutonomyOnline, now.Latest); err != nil {
		return false, err
	}
	if err = writeAutonomyTransition(ctx, tx, AutonomyRecovering, AutonomyOnline, now.Latest, heartbeat, deadline); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
