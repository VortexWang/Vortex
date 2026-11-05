// Package accounting defines the in-process gate and usage reservation contract
// used by the Engine. Implementations own durable or authoritative policy state.
package accounting

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrAlreadyResolved = errors.New("accounting: batch already resolved")
	ErrInvalidUsage    = errors.New("accounting: usage outside reservation")
	ErrDenied          = errors.New("accounting: gate denied operation")
)

type Direction string

const (
	LeftToRight Direction = "left_to_right"
	RightToLeft Direction = "right_to_left"
)

type Flow struct {
	Key    string
	RuleID string
}

type UsageRecord struct {
	BatchID    string
	FlowKey    string
	RuleID     string
	SnapshotID string
	Direction  Direction
	Resolution Resolution
	Reserved   int64
	Used       int64
}

// UsageSink is implemented by the Agent. Engine calls it before settling a
// reservation, keeping durable authority outside the Engine module.
type UsageSink interface {
	ReserveUsage(context.Context, UsageRecord) error
	SettleUsage(context.Context, UsageRecord) error
}

// Gate admits a flow and returns a session policy. Gate implementations may
// enforce connection limits at Open and byte-rate limits in Reserve.
type Gate interface {
	Open(context.Context, Flow) (Session, error)
}

// Session reserves usage before writes and must support concurrent Reserve
// calls when the relay has concurrent directions or UDP responses. Reserve
// must retain the reservation in the gate's authoritative state before it
// returns a Batch. Close releases session-level policy resources, not unknown
// usage reservations.
type Session interface {
	Reserve(context.Context, Direction, int64) (*Batch, error)
	Close() error
}

type Resolution string

const (
	Committed Resolution = "committed"
	Returned  Resolution = "returned"
	Unknown   Resolution = "unknown"
)

type Outcome struct {
	Resolution Resolution
	Reserved   int64
	Used       int64
}

// Resolver must apply the outcome idempotently in its authoritative state. If
// the result is unknown, its implementation must retain the reservation's
// upper bound until reconciliation proves the actual usage.
type Resolver func(context.Context, Outcome) error

// Batch represents one pre-write reservation. Resolve invokes the resolver at
// most once. If the resolver fails, the local state is Unknown; callers must
// preserve the reserved upper bound instead of retrying with a new outcome.
type Batch struct {
	mu       sync.Mutex
	reserved int64
	resolver Resolver
	state    Resolution
	used     int64
}

func NewBatch(reserved int64, resolver Resolver) (*Batch, error) {
	if reserved < 0 || resolver == nil {
		return nil, errors.New("accounting: non-negative reservation and resolver are required")
	}
	return &Batch{reserved: reserved, resolver: resolver}, nil
}

func (b *Batch) Commit(ctx context.Context, used int64) error {
	if used < 0 || used > b.reserved {
		return fmt.Errorf("%w: reserved=%d used=%d", ErrInvalidUsage, b.reserved, used)
	}
	return b.resolve(ctx, Outcome{Resolution: Committed, Reserved: b.reserved, Used: used})
}

func (b *Batch) Return(ctx context.Context) error {
	return b.resolve(ctx, Outcome{Resolution: Returned, Reserved: b.reserved})
}

func (b *Batch) MarkUnknown(ctx context.Context) error {
	return b.resolve(ctx, Outcome{Resolution: Unknown, Reserved: b.reserved})
}

func (b *Batch) State() (Resolution, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, b.used
}

func (b *Batch) Reserved() int64 {
	return b.reserved
}

func (b *Batch) resolve(ctx context.Context, outcome Outcome) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != "" {
		return ErrAlreadyResolved
	}
	err := b.resolver(ctx, outcome)
	if err != nil {
		b.state = Unknown
		b.used = b.reserved
		return err
	}
	b.state = outcome.Resolution
	b.used = outcome.Used
	return nil
}
