package store

import (
	"context"
	"database/sql"
)

// storeTransaction is implemented by both sql.Tx and runtimeTransaction.
// Helpers always execute within the caller's transaction; no result is cached.
type storeTransaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const authorizationRecordQuery = `SELECT authorization_id,parent_authorization_id,source_id,kind,rule_id,rule_revision,path_generation,signer_epoch,boot_id,issued_at,not_before,expires_at,hard_deadline,billing_snapshot_id,amount_bytes,rate_bps,connections,state,issuer_id FROM authorization_records WHERE node_id=? AND authorization_id=?`

// Retain the compiled statement at Store scope, then bind it to each transaction.
// sql.Tx owns the bound statement's lifetime; sql.DB closes the parent statement.
// Preparing on the DB after BeginTx would deadlock our single connection pool.
type runtimeTransaction struct {
	*sql.Tx
	authorizationStatement *sql.Stmt
}

func (s *Store) beginRuntimeTx(ctx context.Context, opts *sql.TxOptions) (*runtimeTransaction, error) {
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &runtimeTransaction{Tx: tx, authorizationStatement: tx.StmtContext(ctx, s.authorizationStatement)}, nil
}

func (tx *runtimeTransaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if query == authorizationRecordQuery {
		return tx.authorizationStatement.QueryRowContext(ctx, args...)
	}
	return tx.Tx.QueryRowContext(ctx, query, args...)
}
