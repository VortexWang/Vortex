package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// BindRuleCommand records the complete immutable business correlation before
// Engine resources are touched. Separate phase task keys can reference the
// same prepared transaction; a changed correlation cannot reuse that task.
func (s *Store) BindRuleCommand(ctx context.Context, task, transaction string, correlation []byte) error {
	if task == "" || len(task) > 128 || len(transaction) != 32 || len(correlation) == 0 || len(correlation) > 4096 {
		return ErrConfigRequestConflict
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS rule_command_bindings(task_key TEXT PRIMARY KEY,transaction_id TEXT NOT NULL,correlation BLOB NOT NULL)`); e != nil {
		return e
	}
	var old string
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT transaction_id,correlation FROM rule_command_bindings WHERE task_key=?`, task).Scan(&old, &raw)
	if e == nil {
		if old != transaction || !bytes.Equal(raw, correlation) {
			return ErrConfigRequestConflict
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO rule_command_bindings(task_key,transaction_id,correlation) VALUES(?,?,?)`, task, transaction, correlation); e != nil {
		return e
	}
	return tx.Commit()
}

// IsPhasedConfig prevents the legacy authorization-resume path from committing
// a transaction for which the backend has sent only Prepare.
func (s *Store) IsPhasedConfig(ctx context.Context, transaction string) (bool, error) {
	var table string
	e := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='rule_command_bindings'`).Scan(&table)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var count int
	e = s.db.QueryRowContext(ctx, `SELECT count(*) FROM rule_command_bindings WHERE transaction_id=?`, transaction).Scan(&count)
	return count > 0, e
}

// SaveRuleConfigRequest isolates monotonic revisions/fences by immutable rule
// identity. config_results remains the common recovery/hold journal; a rule
// transaction must never advance the legacy node-wide applied version.
func (s *Store) SaveRuleConfigRequest(ctx context.Context, rule string, request ConfigResult) (ConfigResult, error) {
	if rule == "" || request.TransactionID == "" || request.Version == 0 || request.FencingGeneration == 0 || len(request.PayloadSHA256) != 32 || len(request.Payload) == 0 {
		return ConfigResult{}, ErrConfigRequestConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConfigResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS rule_config_scopes(transaction_id TEXT PRIMARY KEY REFERENCES config_results(transaction_id),rule_id TEXT NOT NULL)`); err != nil {
		return ConfigResult{}, err
	}
	var storedRule, version, generation, expected, state string
	var payload, digest []byte
	err = tx.QueryRowContext(ctx, `SELECT r.rule_id,c.version_u64,c.fencing_generation,c.expected_version,c.payload,c.payload_sha256,c.state FROM config_results c JOIN rule_config_scopes r USING(transaction_id) WHERE c.transaction_id=?`, request.TransactionID).Scan(&storedRule, &version, &generation, &expected, &payload, &digest, &state)
	if err == nil {
		if storedRule != rule || version != strconv.FormatUint(request.Version, 10) || generation != strconv.FormatUint(request.FencingGeneration, 10) || expected != strconv.FormatUint(request.ExpectedVersion, 10) || !bytes.Equal(payload, request.Payload) || !bytes.Equal(digest, request.PayloadSHA256) {
			return ConfigResult{}, ErrConfigRequestConflict
		}
		request.State = state
		return request, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ConfigResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.version_u64,c.fencing_generation,c.state FROM config_results c JOIN rule_config_scopes r USING(transaction_id) WHERE r.rule_id=?`, rule)
	if err != nil {
		return ConfigResult{}, err
	}
	var active uint64
	for rows.Next() {
		if err = rows.Scan(&version, &generation, &state); err != nil {
			rows.Close()
			return ConfigResult{}, err
		}
		v, e := strconv.ParseUint(version, 10, 64)
		if e != nil {
			rows.Close()
			return ConfigResult{}, e
		}
		g, e := strconv.ParseUint(generation, 10, 64)
		if e != nil {
			rows.Close()
			return ConfigResult{}, e
		}
		if request.FencingGeneration <= g {
			rows.Close()
			return ConfigResult{}, ErrConfigGenerationStale
		}
		if state == "active" && v > active {
			active = v
		}
		if state == "received" || state == "prepared" || state == "committing" || state == "activation-unknown" {
			rows.Close()
			return ConfigResult{}, ErrConfigUnresolved
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return ConfigResult{}, err
	}
	rows.Close()
	if request.Version < active {
		return ConfigResult{}, ErrConfigVersionStale
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO config_results(transaction_id,version,version_u64,fencing_generation,expected_version,payload,payload_sha256,state,reason,updated_at) VALUES(?,0,?,?,?,?,?,'received','',?)`, request.TransactionID, strconv.FormatUint(request.Version, 10), strconv.FormatUint(request.FencingGeneration, 10), strconv.FormatUint(request.ExpectedVersion, 10), request.Payload, request.PayloadSHA256, stamp); err != nil {
		return ConfigResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO rule_config_scopes(transaction_id,rule_id) VALUES(?,?)`, request.TransactionID, rule); err != nil {
		return ConfigResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO activation_holds(transaction_id,generation,state) VALUES(?,?,'held')`, request.TransactionID, strconv.FormatUint(request.FencingGeneration, 10)); err != nil {
		return ConfigResult{}, err
	}
	request.State = "received"
	return request, tx.Commit()
}
