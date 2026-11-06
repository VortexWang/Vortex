package agent

import (
	"context"
	"errors"
	"time"

	"vortex/agent/store"
	accounting "vortex/engine/accounting"
)

// Called with allocator ownership after a whole-grant allocation is denied.
// No Engine capability exists until every child has committed. Failed partial
// collection returns only known-unused children; ambiguous holds stay durable.
func (s *fundingBlockSession) reserveFragments(ctx context.Context, direction accounting.Direction, n int64) (*accounting.Batch, error) {
	if s.fragmentWrites >= 64 {
		return nil, accounting.ErrDenied
	}
	var children []store.UsageReservation
	remaining := uint64(n)
	cleanup := func(cause error) (*accounting.Batch, error) {
		clean, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		for _, child := range children {
			if err := s.inner.gate.node.Store.SettleUsageReservation(clean, child, "returned", 0, nil, 0); err != nil {
				s.inner.gate.fail()
				cause = errors.Join(cause, err)
			}
		}
		return nil, cause
	}
	for remaining > 0 && len(children) < 64 {
		id, err := authorizationBatchID()
		if err != nil {
			s.inner.gate.fail()
			return cleanup(err)
		}
		early, late, trusted := s.inner.gate.config.Clock()
		use := s.inner.gate.use(early, late, trusted, true)
		dir, err := usageDirection(direction)
		if err != nil {
			return cleanup(err)
		}
		request := store.UsageReservation{NodeID: use.NodeID, BootID: use.BootID, BatchID: id, SnapshotID: use.BillingSnapshotID, FlowKey: s.inner.flowKey, RuleID: use.RuleID, Direction: dir, Reserved: remaining}
		child, err := s.inner.gate.node.Store.ReserveRuntimeUsageUpTo(ctx, request, use)
		if err != nil {
			return cleanup(s.admissionError(err))
		}
		children = append(children, child)
		remaining -= child.Reserved
	}
	if remaining != 0 {
		return cleanup(store.ErrAuthorizationDenied)
	}
	s.fragmentWrites++
	return accounting.NewBatch(n, func(ctx context.Context, out accounting.Outcome) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		defer s.signal()
		defer func() { s.fragmentWrites-- }()
		left := uint64(out.Used)
		var failure error
		for _, child := range children {
			resolution := out.Resolution
			used := uint64(0)
			if resolution == accounting.Committed {
				used = min(left, child.Reserved)
				left -= used
				if used == 0 {
					resolution = accounting.Returned
				}
			}
			// After an uncertain settlement, never refund any later child's upper
			// bound; a restart can reconcile those durable identities independently.
			if failure != nil {
				resolution = accounting.Unknown
			}
			if resolution == accounting.Unknown {
				used = child.Reserved
				s.inner.gate.fail()
			}
			record := accounting.UsageRecord{BatchID: child.BatchID, FlowKey: child.FlowKey, RuleID: child.RuleID, SnapshotID: child.SnapshotID, AuthorizationID: child.AuthorizationID, Direction: direction, Reserved: int64(child.Reserved), Used: int64(used), Resolution: resolution}
			settleCtx := ctx
			stop := func() {}
			if failure != nil {
				settleCtx, stop = context.WithTimeout(context.Background(), 2*time.Second)
			}
			err := s.inner.gate.node.SettleUsage(settleCtx, record)
			stop()
			if err != nil {
				s.inner.gate.fail()
				failure = errors.Join(failure, err)
			}
		}
		return failure
	})
}
