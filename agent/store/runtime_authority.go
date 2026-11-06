package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RuntimeAuthorization struct {
	Use         AuthorizationUse
	ShareSource string
}

// OpenRuntimeSession selects only authority matching an already configured
// local rule. Selection, lease checks, available quota and connection limits
// are one transaction. No remote OPEN can create or alter the local rule.
func (s *Store) OpenRuntimeSession(ctx context.Context, scope AuthorizationUse, sessionID, flowKey string) (RuntimeAuthorization, error) {
	var result RuntimeAuthorization
	if sessionID == "" || flowKey == "" || !scope.TrustedTime {
		return result, ErrAuthorizationDenied
	}
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	chosen, err := selectRuntimeAuthorityTx(ctx, tx, scope, 1, nil)
	if err != nil {
		return result, err
	}
	grant, runtime, share := chosen.grant, chosen.runtime, chosen.share
	// All held slots on this source count, including unproven previous boots.
	var count uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND source_id=? AND state='open'`, scope.NodeID, share.SourceID).Scan(&count); err != nil {
		return result, err
	}
	if count >= share.Connections {
		return result, ErrAuthorizationDenied
	}
	use := scope
	use.Kind = "credit_grant"
	use.AuthorizationID = grant.AuthorizationID
	use.RuntimeLeaseID = runtime.AuthorizationID
	use.ShareLeaseID = share.AuthorizationID
	use.BillingSnapshotID = grant.BillingSnapshotID
	if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_sessions(node_id,session_id,boot_id,flow_key,source_id,grant_id,runtime_lease_id,share_lease_id,initial_grant_id,initial_runtime_lease_id,initial_share_lease_id,state) VALUES(?,?,?,?,?,?,?,?,?,?,?, 'open')`, scope.NodeID, sessionID, scope.BootID, flowKey, share.SourceID, grant.AuthorizationID, runtime.AuthorizationID, share.AuthorizationID, grant.AuthorizationID, runtime.AuthorizationID, share.AuthorizationID); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	use.SessionID = sessionID
	use.ExistingSession = true
	return RuntimeAuthorization{Use: use, ShareSource: share.SourceID}, nil
}

func (s *Store) CloseRuntimeSession(ctx context.Context, node, boot, session string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runtime_sessions SET state='closed' WHERE node_id=? AND boot_id=? AND session_id=? AND state='open'`, node, boot, session)
	return err
}

type RateLimited struct{ RetryAfter time.Duration }

func (e *RateLimited) Error() string { return "store: share rate exhausted" }

// One durable token bucket per share source, common to both directions and all
// sessions. Reboot cannot refill it; uncertain time earns no additional tokens.
func consumeRuntimeRate(ctx context.Context, tx storeTransaction, use AuthorizationUse, n uint64) error {
	shareUse := use
	shareUse.Kind = "share_lease"
	shareUse.AuthorizationID = use.ShareLeaseID
	shareUse.SessionID = ""
	shareUse.RuntimeLeaseID = ""
	shareUse.ShareLeaseID = ""
	share, err := checkAuthorizationTx(ctx, tx, shareUse)
	if err != nil {
		return err
	}
	return consumeValidatedRuntimeRateTx(ctx, tx, use, n, share)
}

// The share must have passed checkAuthorizationTx in this same transaction.
// Never use a record cached across transactions or control updates here.
func consumeValidatedRuntimeRateTx(ctx context.Context, tx storeTransaction, use AuthorizationUse, n uint64, share AuthorizationRecord) error {
	if share.RateBPS == 0 {
		return ErrAuthorizationDenied
	}
	burst := share.RateBPS / 8
	if burst < 65536 {
		burst = 65536
	}
	if burst > 1048576 {
		burst = 1048576
	}
	if n > burst {
		return ErrAuthorizationDenied
	}
	var tokens uint64
	var tick int64
	var boot string
	err := tx.QueryRowContext(ctx, `SELECT tokens,last_tick,boot_id FROM runtime_rate_buckets WHERE node_id=? AND source_id=?`, use.NodeID, share.SourceID).Scan(&tokens, &tick, &boot)
	if errors.Is(err, sql.ErrNoRows) {
		tokens = burst
		tick = use.Latest.UnixNano()
	} else if err != nil {
		return err
	}
	if tokens > burst {
		tokens = burst
	}
	if boot != "" && boot != use.BootID {
		return ErrAuthorityHandoffRequired
	}
	delta := use.Earliest.UnixNano() - tick
	if delta > 0 {
		// Saturate before multiplication to avoid overflow after a long outage.
		fill := int64(burst*8*1000000000/share.RateBPS) + 1
		if delta >= fill {
			tokens = burst
		} else {
			earned := uint64(delta) * share.RateBPS / 8000000000
			if earned > burst-tokens {
				tokens = burst
			} else {
				tokens += earned
			}
		}
	}
	if tokens < n {
		return &RateLimited{RetryAfter: time.Duration((n-tokens)*8000000000/share.RateBPS + 1)}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runtime_rate_buckets(node_id,source_id,boot_id,tokens,last_tick) VALUES(?,?,?,?,?) ON CONFLICT(node_id,source_id) DO UPDATE SET boot_id=excluded.boot_id,tokens=excluded.tokens,last_tick=excluded.last_tick`, use.NodeID, share.SourceID, use.BootID, tokens-n, use.Latest.UnixNano())
	return err
}
