package store

import (
	"context"
	"database/sql"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// storeTransaction is implemented by both sql.Tx and runtimeTransaction.
// Helpers always execute within the caller's transaction; no result is cached.
type storeTransaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const authorizationRecordQuery = `SELECT authorization_id,parent_authorization_id,source_id,kind,rule_id,rule_revision,path_generation,signer_epoch,boot_id,issued_at,not_before,expires_at,hard_deadline,billing_snapshot_id,amount_bytes,rate_bps,connections,state,issuer_id,delegation_id,share_reference_id,share_revision FROM authorization_records WHERE node_id=? AND authorization_id=?`

// Retain the compiled statement at Store scope, then bind it to each transaction.
// sql.Tx owns the bound statement's lifetime; sql.DB closes the parent statement.
// Preparing on the DB after BeginTx would deadlock our single connection pool.
type runtimeTransaction struct {
	*sql.Tx
	store    *Store
	parents  map[string]*sql.Stmt
	mu       sync.Mutex
	bound    map[string]*sql.Stmt
	started  time.Time
	wait     time.Duration
	caller   uintptr
	reported sync.Once
	conn     *sql.Conn
	release  func()
	released sync.Once
	// Enabled only by funding admission, which never changes authority rows.
	// Balances, session IDs, time checks and rate remain live in this transaction.
	authorityReads   map[[2]string]authorityRead
	authorityLineage map[[3]string]bool
	demandSamples    *[]demandConsumptionSample
}

type authorityRead struct {
	record AuthorizationRecord
	state  string
}

func (s *Store) beginRuntimeTx(ctx context.Context, opts *sql.TxOptions) (*runtimeTransaction, error) {
	started := time.Now()
	caller, _, _, _ := runtime.Caller(1)
	release, err := s.runtimeQueue.acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		release()
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, opts)
	wait := time.Since(started)
	if err != nil {
		_ = conn.Close()
		release()
		if wait >= 25*time.Millisecond {
			slog.Warn("runtime transaction wait failed", "operation", runtimeOperation(caller), "wait", wait, "error", err)
		}
		return nil, err
	}
	return &runtimeTransaction{Tx: tx, store: s, parents: s.runtimeStatements, bound: make(map[string]*sql.Stmt), started: time.Now(), wait: wait, caller: caller, conn: conn, release: release}, nil
}

func (tx *runtimeTransaction) statement(ctx context.Context, query string) *sql.Stmt {
	parent := tx.parents[query]
	if parent == nil {
		return nil
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if stmt := tx.bound[query]; stmt != nil {
		return stmt
	}
	stmt := tx.Tx.StmtContext(ctx, parent)
	// A cancelled binding must not poison subsequent use of this transaction.
	if ctx.Err() == nil {
		tx.bound[query] = stmt
	}
	return stmt
}

func (tx *runtimeTransaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if stmt := tx.statement(ctx, query); stmt != nil {
		return stmt.QueryRowContext(ctx, args...)
	}
	return tx.Tx.QueryRowContext(ctx, query, args...)
}
func (tx *runtimeTransaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if stmt := tx.statement(ctx, query); stmt != nil {
		return stmt.QueryContext(ctx, args...)
	}
	return tx.Tx.QueryContext(ctx, query, args...)
}
func (tx *runtimeTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if stmt := tx.statement(ctx, query); stmt != nil {
		return stmt.ExecContext(ctx, args...)
	}
	return tx.Tx.ExecContext(ctx, query, args...)
}

func runtimeOperation(pc uintptr) string {
	if f := runtime.FuncForPC(pc); f != nil {
		return f.Name()
	}
	return "unknown"
}

func (tx *runtimeTransaction) report(operation string, err error) {
	tx.reported.Do(func() {
		held := time.Since(tx.started)
		if tx.wait >= 25*time.Millisecond || held >= 25*time.Millisecond {
			slog.Warn("slow runtime transaction", "operation", runtimeOperation(tx.caller), "wait", tx.wait, "held", held, "finish", operation, "error", err)
		}
	})
}
func (tx *runtimeTransaction) Commit() error {
	started := time.Now()
	err := tx.Tx.Commit()
	if elapsed := time.Since(started); elapsed >= 25*time.Millisecond {
		slog.Warn("runtime SQLite commit slow", "operation", runtimeOperation(tx.caller), "commit_elapsed", elapsed, "work_elapsed", started.Sub(tx.started), "error", err)
	}
	tx.finish()
	tx.report("commit", err)
	return err
}
func (tx *runtimeTransaction) Rollback() error {
	err := tx.Tx.Rollback()
	tx.finish()
	tx.report("rollback", err)
	return err
}

// Conn.Close waits for an automatic rollback or an in-flight operation to finish.
// Do not hand the FIFO slot onward merely because Tx reports ErrTxDone.
func (tx *runtimeTransaction) finish() {
	tx.released.Do(func() {
		_ = tx.conn.Close()
		tx.release()
	})
}
