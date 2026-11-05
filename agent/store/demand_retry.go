package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrDemandRetryDeferred = errors.New("store: automatic demand retry deferred")

func checkDemandRetryTx(ctx context.Context, tx *sql.Tx, key []byte, now time.Time) error {
	var reason, denied, retry string
	err := tx.QueryRowContext(ctx, `SELECT reason,denied_at,retry_at FROM demand_retry WHERE scope=?`, key).Scan(&reason, &denied, &retry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	at, e1 := time.Parse(time.RFC3339Nano, denied)
	until, e2 := time.Parse(time.RFC3339Nano, retry)
	if e1 != nil || e2 != nil || now.Before(at) {
		return ErrAuthorityTimeUncertain
	}
	// Invalid/expired immutable scopes need replacement provisioning, not a
	// stream of new request IDs. Funding shortages can recover with backoff.
	if reason != "insufficient_funds" || now.Before(until) {
		return ErrDemandRetryDeferred
	}
	return nil
}

func recordDemandRetryTx(ctx context.Context, tx *sql.Tx, key []byte, reason string, now time.Time) error {
	var failures int
	var denied string
	err := tx.QueryRowContext(ctx, `SELECT failures,denied_at FROM demand_retry WHERE scope=?`, key).Scan(&failures, &denied)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		at, err := time.Parse(time.RFC3339Nano, denied)
		if err != nil || now.Before(at) {
			return ErrAuthorityTimeUncertain
		}
	}
	if failures < 0 || failures > 7 {
		return ErrAuthorizationDenied
	}
	if failures < 7 {
		failures++
	}
	delay := time.Second << uint(failures-1)
	if delay > time.Minute {
		delay = time.Minute
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO demand_retry(scope,reason,failures,denied_at,retry_at) VALUES(?,?,?,?,?) ON CONFLICT(scope) DO UPDATE SET reason=excluded.reason,failures=excluded.failures,denied_at=excluded.denied_at,retry_at=excluded.retry_at`, key, reason, failures, now.UTC().Format(time.RFC3339Nano), now.Add(delay).UTC().Format(time.RFC3339Nano))
	return err
}
