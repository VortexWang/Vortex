package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"vortex/agent/store"
	engineaccounting "vortex/engine/accounting"
)

// AuthorizationClock returns conservative bounds. trusted must be false until
// the Agent has recovered and validated its time anchor.
type AuthorizationClock func() (earliest, latest time.Time, trusted bool)

type AuthorizationGateConfig struct {
	SessionID       string
	RuntimeLeaseID  string
	ShareLeaseID    string
	AuthorizationID string
	RuleID          string
	RuleRevision    uint64
	PathGeneration  uint64
	SnapshotID      string
	Clock           AuthorizationClock
}

// NewAuthorizationGate creates an Agent-to-Engine credit-grant adapter.
// Runtime lease admission and authenticated clock setup remain separate. Every write reservation consumes a durable authorization
// upper bound before Engine can write; settlement refunds only proven unused
// bytes. Unknown write outcomes remain charged.
func (n *Node) NewAuthorizationGate(c AuthorizationGateConfig) (engineaccounting.Gate, error) {
	if n == nil || n.Store == nil || c.AuthorizationID == "" || c.RuleID == "" || c.RuleRevision == 0 || c.PathGeneration == 0 || c.SnapshotID == "" || c.Clock == nil {
		return nil, fmt.Errorf("agent: durable authorization gate configuration is incomplete")
	}
	return &authorizationGate{node: n, config: c}, nil
}

// NewLeasedAuthorizationGate requires runtime and share leases in addition to
// credit. Admission and every byte reservation validate all three in the same
// SQLite snapshot; passing a credit grant alone cannot satisfy this boundary.
func (n *Node) NewLeasedAuthorizationGate(c AuthorizationGateConfig) (engineaccounting.Gate, error) {
	if c.Clock == nil {
		c.Clock = n.TrustedTime
	}
	if c.RuntimeLeaseID == "" || c.ShareLeaseID == "" {
		return nil, fmt.Errorf("agent: runtime and share lease identities are required")
	}
	return n.NewAuthorizationGate(c)
}

type authorizationGate struct {
	node        *Node
	config      AuthorizationGateConfig
	faulted     atomic.Bool
	parentFault *atomic.Bool
}

func (g *authorizationGate) use(earliest, latest time.Time, trusted, existing bool) store.AuthorizationUse {
	return store.AuthorizationUse{
		SessionID: g.config.SessionID, NodeID: g.node.NodeID, BootID: g.node.BootID, AuthorizationID: g.config.AuthorizationID,
		RuntimeLeaseID: g.config.RuntimeLeaseID, ShareLeaseID: g.config.ShareLeaseID,
		Kind: "credit_grant", RuleID: g.config.RuleID, RuleRevision: g.config.RuleRevision,
		PathGeneration: g.config.PathGeneration, BillingSnapshotID: g.config.SnapshotID,
		Earliest: earliest, Latest: latest, TrustedTime: trusted, ExistingSession: existing,
	}
}

func (g *authorizationGate) Open(ctx context.Context, flow engineaccounting.Flow) (engineaccounting.Session, error) {
	if g.isFaulted() || flow.Key == "" || flow.RuleID != "" && flow.RuleID != g.config.RuleID {
		return nil, engineaccounting.ErrDenied
	}
	earliest, latest, trusted := g.config.Clock()
	if _, err := g.node.Store.CheckStoredAuthorization(ctx, g.use(earliest, latest, trusted, false)); err != nil {
		return nil, fmt.Errorf("agent: durable grant denied: %w", err)
	}
	return &authorizationSession{gate: g, flowKey: flow.Key}, nil
}

type authorizationSession struct {
	gate    *authorizationGate
	flowKey string
	closed  atomic.Bool
}

func (s *authorizationSession) Reserve(ctx context.Context, direction engineaccounting.Direction, n int64) (*engineaccounting.Batch, error) {
	for {
		batch, err := withAuthorityWait(ctx, s.gate.node, RuntimeScope{RuleID: s.gate.config.RuleID, RuleRevision: s.gate.config.RuleRevision, PathGeneration: s.gate.config.PathGeneration}, func(waitCtx context.Context) (*engineaccounting.Batch, error) {
			return s.reserveOnce(waitCtx, direction, n)
		})
		var limited *store.RateLimited
		if !errors.As(err, &limited) {
			return batch, err
		}
		timer := time.NewTimer(limited.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *authorizationSession) reserveOnce(ctx context.Context, direction engineaccounting.Direction, n int64) (*engineaccounting.Batch, error) {
	if s.gate.isFaulted() || s.closed.Load() || n < 1 || n > 4<<20 || (direction != engineaccounting.LeftToRight && direction != engineaccounting.RightToLeft) {
		return nil, engineaccounting.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	early, late, trusted := s.gate.config.Clock()
	batchID, err := authorizationBatchID()
	if err != nil {
		s.gate.fail()
		return nil, err
	}
	record := engineaccounting.UsageRecord{
		BatchID: batchID, FlowKey: s.flowKey, RuleID: s.gate.config.RuleID,
		SnapshotID: s.gate.config.SnapshotID, AuthorizationID: s.gate.config.AuthorizationID,
		RuleRevision: s.gate.config.RuleRevision, PathGeneration: s.gate.config.PathGeneration,
		Direction: direction, Reserved: n, Earliest: early, Latest: late,
		TrustedTime: trusted, ExistingSession: true,
	}
	reservation := store.UsageReservation{
		NodeID: s.gate.node.NodeID, BootID: s.gate.node.BootID, BatchID: record.BatchID, AuthorizationID: record.AuthorizationID,
		SnapshotID: record.SnapshotID, FlowKey: record.FlowKey, RuleID: record.RuleID,
		Direction: "egress", Reserved: uint64(record.Reserved),
	}
	if direction == engineaccounting.RightToLeft {
		reservation.Direction = "ingress"
	}
	var effectiveID string
	if s.gate.config.SessionID != "" {
		effectiveID, err = s.gate.node.Store.ReserveRuntimeUsage(ctx, reservation, s.gate.use(early, late, trusted, true))
	} else {
		effectiveID, err = s.gate.node.Store.ReserveAuthorizedUsage(ctx, reservation, s.gate.use(early, late, trusted, true))
	}
	if err != nil {
		var limited *store.RateLimited
		if !errors.Is(err, store.ErrAuthorizationDenied) && !errors.Is(err, store.ErrAuthorityTimeUncertain) && !errors.As(err, &limited) {
			s.gate.fail()
		}
		return nil, err
	}
	record.AuthorizationID = effectiveID
	return engineaccounting.NewBatch(n, func(resolveCtx context.Context, outcome engineaccounting.Outcome) error {
		record.Resolution = outcome.Resolution
		record.Reserved = outcome.Reserved
		record.Used = outcome.Used
		err := s.gate.node.SettleUsage(resolveCtx, record)
		if err != nil {
			s.gate.fail()
		}
		return err
	})
}

func authorizationBatchID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (s *authorizationSession) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	if s.gate.config.SessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.gate.node.Store.CloseRuntimeSession(ctx, s.gate.node.NodeID, s.gate.node.BootID, s.gate.config.SessionID); err != nil {
			s.gate.fail()
			return err
		}
	}
	return nil
}

func (g *authorizationGate) isFaulted() bool {
	return g.faulted.Load() || g.parentFault != nil && g.parentFault.Load()
}
func (g *authorizationGate) fail() {
	g.faulted.Store(true)
	if g.parentFault != nil {
		g.parentFault.Store(true)
	}
}
