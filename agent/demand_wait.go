package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
	"vortex/agent/store"
)

const missingAuthorityWait = 200 * time.Millisecond

type demandSubscription struct {
	scopes []AdaptiveDemandConfig
	wake   chan struct{}
}

// A bounded live waiter is an estimate hint, never spending authority.
type waitingDemandWrite struct {
	scope RuntimeScope
	bytes uint64
}

func (n *Node) subscribeDemands(configs []AdaptiveDemandConfig) (<-chan struct{}, func(), error) {
	n.demandMu.Lock()
	defer n.demandMu.Unlock()
	if len(configs) > 64 || len(n.demandSubscriptions) >= 64 {
		return nil, nil, fmt.Errorf("agent: demand subscription limit exceeded")
	}
	for _, c := range configs {
		if c.Scope.NodeID != n.NodeID || c.Scope.BootID != n.BootID {
			return nil, nil, fmt.Errorf("agent: demand subscription identity mismatch")
		}
	}
	if n.demandSubscriptions == nil {
		n.demandSubscriptions = make(map[*demandSubscription]struct{})
	}
	sub := &demandSubscription{scopes: append([]AdaptiveDemandConfig(nil), configs...), wake: make(chan struct{}, 1)}
	n.demandSubscriptions[sub] = struct{}{}
	return sub.wake, func() { n.demandMu.Lock(); delete(n.demandSubscriptions, sub); n.demandMu.Unlock() }, nil
}

func (n *Node) wakeDemand(scope RuntimeScope) bool {
	n.demandMu.Lock()
	defer n.demandMu.Unlock()
	found := false
	for sub := range n.demandSubscriptions {
		for _, c := range sub.scopes {
			if c.Scope.RuleID == scope.RuleID && c.Scope.RuleRevision == scope.RuleRevision && c.Scope.PathGeneration == scope.PathGeneration {
				found = true
				select {
				case sub.wake <- struct{}{}:
				default:
				}
				break
			}
		}
	}
	return found
}

// Retry only a missing durable authorization, only while an opt-in control
// consumer for this exact runtime scope exists. The same deadline covers the
// first transactional attempt and every retry; it is never reset after denial.
// Each retry re-enters the actual transactional gate; a wakeup is not authority.
func withAuthorityWait[T any](ctx context.Context, n *Node, scope RuntimeScope, attempt func(context.Context) (T, error), writeBytes ...uint64) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, missingAuthorityWait)
	defer cancel()
	value, err := attempt(waitCtx)
	if !errors.Is(err, store.ErrAuthorizationDenied) || waitCtx.Err() != nil {
		return value, err
	}
	n.demandMu.Lock()
	if n.demandWaiters == nil {
		n.demandWaiters = make(chan struct{}, 128)
	}
	slots := n.demandWaiters
	n.demandMu.Unlock()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		return value, err
	}
	if len(writeBytes) > 0 && writeBytes[0] > 0 {
		write := &waitingDemandWrite{scope: scope, bytes: writeBytes[0]}
		n.demandMu.Lock()
		if n.demandWrites == nil {
			n.demandWrites = make(map[*waitingDemandWrite]struct{})
		}
		n.demandWrites[write] = struct{}{}
		n.demandMu.Unlock()
		defer func() { n.demandMu.Lock(); delete(n.demandWrites, write); n.demandMu.Unlock() }()
	}
	if !n.wakeDemand(scope) {
		return value, err
	}

	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-waitCtx.Done():
			var zero T
			return zero, waitCtx.Err()
		case <-timer.C:
			value, err = attempt(waitCtx)
			if !errors.Is(err, store.ErrAuthorizationDenied) {
				return value, err
			}
			if !n.wakeDemand(scope) {
				return value, err
			}
		}
	}
}
