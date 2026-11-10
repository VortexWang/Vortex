package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// RuntimeFundingWrite commits either one block or a complete set of per-grant
// children. Reclaimed lists only idle tails returned by that same commit.
type RuntimeFundingWrite struct {
	Block     UsageReservation
	Children  []UsageReservation
	Reclaimed []UsageReservation
}

// ReserveAndAdmitRuntimeFundingWrite keeps definitive allocation fallbacks in
// one writer turn. The caller exclusively owns the session and proves every
// supplied tail has no in-flight writer. No partial capability escapes.
func (s *Store) ReserveAndAdmitRuntimeFundingWrite(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64, replacement, opposite []UsageReservation, ids []string, clocks ...func() (time.Time, time.Time, bool)) (RuntimeFundingWrite, error) {
	var result RuntimeFundingWrite
	if len(clocks) > 1 || len(clocks) == 1 && clocks[0] == nil || s.owner != "agent" || minimum == 0 || minimum > r.Reserved || r.Reserved > 4<<20 || len(replacement) > 1 || len(opposite) > 1 || len(ids) == 0 || len(ids) > 64 {
		return result, ErrAuthorizationDenied
	}
	seen := map[string]bool{r.BatchID: true}
	for _, id := range ids {
		if id == "" || seen[id] {
			return result, ErrUsageBatchConflict
		}
		seen[id] = true
	}
	all := append(append([]UsageReservation(nil), replacement...), opposite...)
	for i, tail := range all {
		if tail.BatchID == r.BatchID || (i > 0 && tail.BatchID == all[0].BatchID) {
			return result, ErrUsageBatchConflict
		}
	}
	err := s.enqueueFundingOperation(ctx, len(ids), func(tx *runtimeTransaction) error {
		// Read the same authenticated clock after acquiring the connection.
		// A grant installed while this request queued must be checked against
		// current conservative bounds, not the pre-queue not_before snapshot.
		// The context deadline and all identity/lineage checks still apply.
		if len(clocks) == 1 {
			scope.Earliest, scope.Latest, scope.TrustedTime = clocks[0]()
		}
		possible, err := fundingWriteMayFitTx(ctx, tx, scope, minimum, all)
		if err != nil {
			return err
		}
		if !possible {
			return ErrAuthorizationDenied
		}
		tryBlock := func(tails []UsageReservation) error {
			encoded, _ := json.Marshal(tails)
			bounds := &fundingBlockRange{minimum: minimum, preferred: r.Reserved, idle: tails, idleDigest: fmt.Sprintf("%x", sha256.Sum256(encoded)), admit: true}
			id, err := s.reserveRuntimeUsageDirect(ctx, r, scope, true, bounds, tx)
			if err == nil {
				result.Block = r
				result.Block.AuthorizationID = id
				result.Block.Reserved = bounds.allocated
				result.Reclaimed = tails
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT funding_choice"); err != nil {
			return err
		}
		reset := func() error { _, err := tx.ExecContext(ctx, "ROLLBACK TO funding_choice"); return err }
		err = tryBlock(replacement)
		if errors.Is(err, ErrAuthorizationDenied) && len(opposite) > 0 {
			if e := reset(); e != nil {
				return e
			}
			err = tryBlock(all)
		}
		if errors.Is(err, ErrAuthorizationDenied) {
			if e := reset(); e != nil {
				return e
			}
			remaining := minimum
			for i, id := range ids {
				child := r
				child.BatchID = id
				child.Reserved = remaining
				bounds := &fundingBlockRange{minimum: 1, preferred: remaining}
				if i == 0 {
					bounds.idle = all
				}
				grant, e := s.reserveRuntimeUsageDirect(ctx, child, scope, false, bounds, tx)
				if e != nil {
					return e
				}
				child.AuthorizationID = grant
				child.Reserved = bounds.allocated
				result.Children = append(result.Children, child)
				remaining -= child.Reserved
				if remaining == 0 {
					result.Reclaimed = all
					err = nil
					break
				}
			}
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "RELEASE funding_choice")
		return err
	})
	if err != nil {
		return RuntimeFundingWrite{}, err
	}
	return result, nil
}

// This is only an impossibility filter. It deliberately overestimates usable
// credit: coarse expiry, all matching sources and all supplied idle tails.
// A positive result MUST still execute the complete durable allocator checks.
const fundingWriteUpperBoundQuery = `SELECT b.remaining_bytes FROM authorization_records a JOIN authorization_balances b ON b.node_id=a.node_id AND b.authorization_id=a.authorization_id WHERE a.node_id=? AND a.boot_id=? AND a.rule_id=? AND a.rule_revision=? AND a.path_generation=? AND a.kind='credit_grant' AND a.state='active' AND unixepoch(a.expires_at)>=unixepoch(?)`

func fundingWriteMayFitTx(ctx context.Context, tx storeTransaction, scope AuthorizationUse, minimum uint64, idle []UsageReservation) (bool, error) {
	if !scope.TrustedTime || scope.Earliest.IsZero() || scope.Latest.Before(scope.Earliest) {
		return false, ErrAuthorityTimeUncertain
	}
	var upper uint64
	for _, tail := range idle {
		if tail.Reserved >= minimum-upper {
			return true, nil
		}
		upper += tail.Reserved
	}
	rows, err := tx.QueryContext(ctx, fundingWriteUpperBoundQuery, scope.NodeID, scope.BootID, scope.RuleID, strconv.FormatUint(scope.RuleRevision, 10), strconv.FormatUint(scope.PathGeneration, 10), scope.Latest.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		amount, err := strconv.ParseUint(raw, 10, 64)
		// Leave corrupt/unexpected authority to the original allocator; this
		// inexpensive filter never turns malformed data into a spending decision.
		if err != nil {
			return true, nil
		}
		if amount >= minimum-upper {
			return true, nil
		}
		upper += amount
	}
	return false, rows.Err()
}
