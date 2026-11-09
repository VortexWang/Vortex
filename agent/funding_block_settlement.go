package agent

import (
	"context"
	"sync"
	"time"

	"vortex/agent/store"
	accounting "vortex/engine/accounting"
)

type fundingSettlementRequest struct {
	ctx       context.Context
	direction accounting.Direction
	block     *sessionFundingBlock
	outcome   accounting.Outcome
	result    chan error
}

func (s *fundingBlockSession) resolve(ctx context.Context, direction accounting.Direction, b *sessionFundingBlock, out accounting.Outcome) error {
	if out.Resolution != accounting.Committed || out.Used == 0 {
		return s.resolveDirect(ctx, direction, b, out)
	}
	r := &fundingSettlementRequest{ctx: ctx, direction: direction, block: b, outcome: out, result: make(chan error, 1)}
	s.settlementMu.Lock()
	if s.settlementCount >= 64 {
		s.settlementMu.Unlock()
		return s.resolveDirect(ctx, direction, b, out)
	}
	s.settlementCount++
	s.settlements = append(s.settlements, r)
	if !s.settlementRunning {
		s.settlementRunning = true
		go s.drainSettlements()
	}
	s.settlementMu.Unlock()
	// Do not return on cancellation before the worker has classified the durable
	// outcome and retained/frozen its bound. The transaction observes ctx below.
	return <-r.result
}
func (s *fundingBlockSession) drainSettlements() {
	timer := time.NewTimer(time.Millisecond)
	<-timer.C
	for {
		s.settlementMu.Lock()
		if len(s.settlements) == 0 {
			s.settlementRunning = false
			s.settlementMu.Unlock()
			return
		}
		group := s.settlements
		s.settlements = nil
		s.settlementMu.Unlock()
		err := s.commitSettlementGroup(group)
		for _, r := range group {
			r.result <- err
		}
		s.settlementMu.Lock()
		s.settlementCount -= len(group)
		s.settlementMu.Unlock()
	}
}
func (s *fundingBlockSession) commitSettlementGroup(group []*fundingSettlementRequest) (resultErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.signal()
	// Retain all in-flight upper bounds until commit or fail-closed classification.
	defer func() {
		if resultErr != nil {
			for _, r := range group {
				r.block.poisoned = true
				r.block.inFlight -= uint64(r.outcome.Reserved)
			}
			s.inner.gate.fail()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks sync.WaitGroup
	stops := make([]func() bool, 0, len(group))
	for _, r := range group {
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	for _, r := range group {
		callbacks.Add(1)
		stops = append(stops, context.AfterFunc(r.ctx, func() { cancel(); callbacks.Done() }))
	}
	defer func() {
		for _, stop := range stops {
			if stop() {
				callbacks.Done()
			}
		}
		callbacks.Wait()
	}()
	residuals := make(map[*sessionFundingBlock]store.UsageReservation)
	entries := make([]store.RuntimeBlockCheckpoint, 0, len(group))
	for _, r := range group {
		if r.block.poisoned {
			return accounting.ErrDenied
		}
		residual, ok := residuals[r.block]
		if !ok {
			residual = r.block.reservation
		}
		if r.outcome.Used < 1 || uint64(r.outcome.Used) > residual.Reserved {
			return accounting.ErrInvalidUsage
		}
		id, err := authorizationBatchID()
		if err != nil {
			return err
		}
		record := accounting.UsageRecord{BatchID: id, RuleID: residual.RuleID, SnapshotID: residual.SnapshotID, AuthorizationID: residual.AuthorizationID}
		payload, err := marshalUsage(record, residual.Direction, uint64(r.outcome.Used))
		if err != nil {
			return err
		}
		entries = append(entries, store.RuntimeBlockCheckpoint{Block: residual, CheckpointID: id, Used: uint64(r.outcome.Used), Payload: payload})
		residual.Reserved -= uint64(r.outcome.Used)
		residuals[r.block] = residual
	}
	if err := s.inner.gate.node.Store.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
		return err
	}
	for block, residual := range residuals {
		block.reservation = residual
	}
	for _, r := range group {
		r.block.inFlight -= uint64(r.outcome.Reserved)
	}
	// From here checkpoint success is durable. Tail-return errors fault the gate
	// but must not decrement these in-flight counters a second time.
	for block := range residuals {
		direction := accounting.LeftToRight
		if block.reservation.Direction == "ingress" {
			direction = accounting.RightToLeft
		}
		if block.reservation.Reserved == 0 {
			delete(s.blocks, direction)
			continue
		}
		if block.inFlight == 0 {
			if s.closed {
				cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
				err := s.release(cleanup, block)
				stop()
				if err != nil {
					block.poisoned = true
					s.inner.gate.fail()
				} else {
					delete(s.blocks, direction)
				}
			} else {
				s.armIdle(direction, block)
			}
		}
	}
	return nil
}
