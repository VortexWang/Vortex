package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// BeginBootRecovery records old activation evidence before runtime startup.
// Only same-kernel, same-PID-namespace OS death evidence may release a prior
// process's configuration holds. A different boot_id never supplies that proof.
func (s *Store) BeginBootRecovery(ctx context.Context, boot string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.transaction_id,c.fencing_generation,c.state,COALESCE(w.witness,X'') FROM config_results c LEFT JOIN activation_processes w ON w.transaction_id=c.transaction_id WHERE c.state NOT IN ('released','aborted','prepare-failed','commit-failed') ORDER BY c.transaction_id`)
	if err != nil {
		return err
	}
	type pending struct {
		ID, Generation, State string
		Witness               []byte
		Stopped               bool
	}
	var records []pending
	for rows.Next() {
		var r pending
		if err = rows.Scan(&r.ID, &r.Generation, &r.State, &r.Witness); err != nil {
			rows.Close()
			return err
		}
		if len(r.Witness) > 0 {
			r.Stopped, err = processStopped(r.Witness)
			if err != nil {
				rows.Close()
				return err
			}
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return tx.Commit()
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, r := range records {
		state, proof := "activation-unknown", "boot-recovery-unproven"
		if r.Stopped {
			state, proof = "released", "os-process-stopped"
			if _, err = tx.ExecContext(ctx, `UPDATE activation_holds SET state='released' WHERE transaction_id=?`, r.ID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE config_results SET state=?,updated_at=? WHERE transaction_id=?`, state, stamp, r.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO activation_observations(transaction_id,generation,execution_state,observed_at) VALUES(?,?,?,?)`, r.ID, r.Generation, proof, stamp); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(struct {
		BootID   string
		Previous []pending
	}{boot, records})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO durable_events(event_id,owner,kind,payload,created_at) VALUES(?,?,'boot-recovery',?,?)`, "recovery-"+boot, s.owner, raw, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

// RecoveryEvents are replayed on each authenticated connection. The event ID
// stays fixed; delivery is not a financial ACK and never clears old evidence.
func (s *Store) RecoveryEvents(ctx context.Context, after string) ([]RecoveryEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,payload FROM durable_events WHERE kind='boot-recovery' AND event_id>? ORDER BY event_id LIMIT 32`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoveryEvent
	for rows.Next() {
		var e RecoveryEvent
		if err = rows.Scan(&e.ID, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type RecoveryEvent struct {
	ID      string
	Payload []byte
}

func (s *Store) PreparedConfigs(ctx context.Context) ([]ConfigResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT transaction_id FROM config_results WHERE state='prepared' ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var records []ConfigResult
	for _, id := range ids {
		r, err := s.GetConfigResult(ctx, id)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func (s *Store) PersistRecoveryEvent(ctx context.Context, node, id string, payload []byte) error {
	if s.owner != "hub" || node == "" || id == "" || len(payload) > 1<<20 {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prior []byte
	key := "node-recovery/" + node + "/" + id
	err = tx.QueryRowContext(ctx, `SELECT payload FROM durable_events WHERE event_id=?`, key).Scan(&prior)
	if err == nil {
		if !bytes.Equal(prior, payload) {
			return ErrConfigRequestConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO durable_events(event_id,owner,kind,payload,created_at) VALUES(?,?,'node-boot-recovery',?,?)`, key, s.owner, payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
