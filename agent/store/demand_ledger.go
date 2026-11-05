package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

func demandScopeFor(node string, r AuthorizationRecord) DemandScope {
	return DemandScope{HubID: r.IssuerID, DelegationID: r.ParentAuthorizationID, NodeID: node, BootID: r.BootID, SourceID: r.SourceID, SnapshotID: r.BillingSnapshotID, RuleID: r.RuleID, SignerEpoch: r.SignerEpoch, RuleRevision: r.RuleRevision, PathGeneration: r.PathGeneration}
}

// Called once inside a successful settlement transaction, after its duplicate
// check. Unknown upper bounds, returns and replayed settlements never add rate.
func recordDemandConsumptionTx(ctx context.Context, tx *sql.Tx, node, grant string, used uint64, at time.Time) error {
	r, _, err := readAuthorizationTx(ctx, tx, node, grant)
	if err != nil {
		return err
	}
	key, err := json.Marshal(demandScopeFor(node, r))
	if err != nil {
		return err
	}
	second := at.Unix()
	if second < 0 {
		return ErrAuthorityTimeUncertain
	}
	var latest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(second) FROM demand_consumption WHERE scope=?`, key).Scan(&latest); err != nil {
		return err
	}
	if latest.Valid && second < latest.Int64 {
		return ErrAuthorityTimeUncertain
	}
	var previousSecond int64
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT second,bytes FROM demand_consumption WHERE scope=? AND slot=?`, key, second%60).Scan(&previousSecond, &raw)
	var total uint64
	if err == nil && previousSecond == second {
		total, err = strconv.ParseUint(raw, 10, 64)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if total > math.MaxInt64 || used > math.MaxInt64-total {
		total = math.MaxInt64
	} else {
		total += used
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO demand_consumption(scope,slot,second,bytes) VALUES(?,?,?,?) ON CONFLICT(scope,slot) DO UPDATE SET second=excluded.second,bytes=excluded.bytes`, key, second%60, second, strconv.FormatUint(total, 10))
	return err
}

type DemandLedger struct{ Available, MinuteBytes uint64 }

// ReadDemandLedger is a planning snapshot, never a capability to write. It sums
// only still-valid credit in exactly this immutable scope. Held/unknown bytes
// are absent from remaining balances. Runtime/share checks still govern writes.
// Authority time is conservative trusted time; sampleTime is the local clock
// shared with settlement's rate buckets, not an authority timestamp.
func (s *Store) ReadDemandLedger(ctx context.Context, scope DemandScope, now IssuerTime, sampleTime time.Time) (DemandLedger, error) {
	var result DemandLedger
	if s.owner != "agent" {
		return result, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return result, err
	}
	if sampleTime.IsZero() || sampleTime.Unix() < 0 {
		return result, ErrAuthorityTimeUncertain
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT authorization_id FROM authorization_records WHERE node_id=? AND source_id=? AND kind='credit_grant' AND state='active'`, scope.NodeID, scope.SourceID)
	if err != nil {
		return result, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
		if len(ids) > 4096 {
			rows.Close()
			return result, ErrAuthorizationDenied
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		r, err := checkAuthorizationTx(ctx, tx, AuthorizationUse{NodeID: scope.NodeID, BootID: scope.BootID, AuthorizationID: id, Kind: "credit_grant", RuleID: scope.RuleID, RuleRevision: scope.RuleRevision, PathGeneration: scope.PathGeneration, BillingSnapshotID: scope.SnapshotID, Earliest: now.Earliest, Latest: now.Latest, TrustedTime: now.Trusted, ExistingSession: true})
		if errors.Is(err, ErrAuthorizationDenied) {
			continue
		}
		if err != nil {
			return DemandLedger{}, err
		}
		if demandScopeFor(scope.NodeID, r) != scope {
			continue
		}
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, scope.NodeID, id).Scan(&raw); err != nil {
			return DemandLedger{}, err
		}
		remaining, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || remaining > r.AmountBytes || remaining > math.MaxInt64-result.Available {
			return DemandLedger{}, ErrAuthorizationDenied
		}
		result.Available += remaining
	}
	key, err := json.Marshal(scope)
	if err != nil {
		return DemandLedger{}, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT second,bytes FROM demand_consumption WHERE scope=?`, key)
	if err != nil {
		return DemandLedger{}, err
	}
	second := sampleTime.Unix()
	for rows.Next() {
		var observed int64
		var raw string
		if err := rows.Scan(&observed, &raw); err != nil {
			rows.Close()
			return DemandLedger{}, err
		}
		if observed > second {
			rows.Close()
			return DemandLedger{}, ErrAuthorityTimeUncertain
		}
		if observed <= second-60 {
			continue
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			rows.Close()
			return DemandLedger{}, err
		}
		if n > math.MaxInt64-result.MinuteBytes {
			result.MinuteBytes = math.MaxInt64
		} else {
			result.MinuteBytes += n
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return DemandLedger{}, err
	}
	if err := tx.Commit(); err != nil {
		return DemandLedger{}, err
	}
	return result, nil
}
