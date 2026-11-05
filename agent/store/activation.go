package store

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// ObserveActivation persists a query result and its resource disposition in one
// FULL transaction. Unknown/missing Engine state never releases a hold.
func (s *Store) ObserveActivation(ctx context.Context, id string, generation uint64, state string) error {
	if generation == 0 {
		return ErrConfigRequestConflict
	}
	switch state {
	case "active", "prepared", "committing", "activation-unknown", "aborted", "released", "missing":
	default:
		return ErrConfigRequestConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var stored, previous, version string
	if err := tx.QueryRowContext(ctx, `SELECT fencing_generation,state,version_u64 FROM config_results WHERE transaction_id=?`, id).Scan(&stored, &previous, &version); err != nil {
		return err
	}
	if stored != strconv.FormatUint(generation, 10) {
		return ErrConfigRequestConflict
	}
	if previous == "released" && state != "released" && state != "aborted" {
		return ErrConfigRequestConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO activation_observations(transaction_id,generation,execution_state,observed_at) VALUES(?,?,?,?)`, id, stored, state, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO activation_holds(transaction_id,generation,state) VALUES(?,?,'held') ON CONFLICT DO NOTHING`, id, stored); err != nil {
		return err
	}
	target := "activation-unknown"
	switch state {
	case "active":
		target = "active"
		// Never regress an already applied version using a delayed observation.
		var current string
		_ = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='applied_version'`).Scan(&current)
		old, _ := strconv.ParseUint(current, 10, 64)
		next, err := strconv.ParseUint(version, 10, 64)
		if err != nil || next < old {
			return fmt.Errorf("store: stale active observation")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_state(key,value,updated_at) VALUES('applied_version',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, version, stamp); err != nil {
			return err
		}
	case "aborted", "released":
		if previous == "active" {
			return ErrConfigRequestConflict
		}
		target = "released"
		if _, err := tx.ExecContext(ctx, `UPDATE activation_holds SET state='released' WHERE transaction_id=?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE config_results SET state=?,updated_at=? WHERE transaction_id=?`, target, stamp, id); err != nil {
		return err
	}
	return tx.Commit()
}
