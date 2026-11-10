package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
func (s *Store) ReserveAndAdmitRuntimeFundingWrite(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64, replacement, opposite []UsageReservation, ids []string) (RuntimeFundingWrite, error) {
	var result RuntimeFundingWrite
	if s.owner != "agent" || minimum == 0 || minimum > r.Reserved || r.Reserved > 4<<20 || len(replacement) > 1 || len(opposite) > 1 || len(ids) == 0 || len(ids) > 64 {
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
		err := tryBlock(replacement)
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
