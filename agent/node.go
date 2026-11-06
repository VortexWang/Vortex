package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"vortex/agent/store"
)

type Engine interface {
	Start(context.Context) error
	Stop(context.Context) error
}

type NoopEngine struct{}

func (NoopEngine) Start(context.Context) error { return nil }
func (NoopEngine) Stop(context.Context) error  { return nil }

type Node struct {
	demandMu            sync.Mutex
	demandSubscriptions map[*demandSubscription]struct{}
	demandWaiters       chan struct{}
	bootStarted         time.Time
	clock               *AuthorityClock
	NodeID              string
	BootID              string
	Store               *store.Store
	Engine              Engine
}

func New(nodeID string, s *store.Store, e Engine) (*Node, error) {
	if nodeID == "" || s == nil {
		return nil, fmt.Errorf("agent: node id and store are required")
	}
	if e == nil {
		e = NoopEngine{}
	}
	bootStarted := time.Now()
	var boot [16]byte
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, fmt.Errorf("agent: boot id: %w", err)
	}
	encodedBoot := hex.EncodeToString(boot[:])
	bootID := encodedBoot[:8] + "-" + encodedBoot[8:12] + "-" + encodedBoot[12:16] + "-" + encodedBoot[16:20] + "-" + encodedBoot[20:]
	if err := s.MarkPreviousBootAuthorizationsSettlementOnly(context.Background(), nodeID, bootID); err != nil {
		return nil, fmt.Errorf("agent: fence previous-boot authorizations before Engine start: %w", err)
	}
	node := &Node{NodeID: nodeID, BootID: bootID, Store: s, Engine: e, clock: newAuthorityClock(), bootStarted: bootStarted}
	if err := node.recoverUsageReservations(context.Background()); err != nil {
		return nil, fmt.Errorf("agent: recover usage reservations before Engine start: %w", err)
	}
	if runtime, ok := e.(*RuntimeEngine); ok {
		if err := runtime.SetAccountingGate(node.newRuntimeAuthorityGate(runtime.scope, node.TrustedTime)); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func (n *Node) Start(ctx context.Context) error {
	if err := n.Store.AppendEvent(ctx, "boot-"+n.BootID, "boot", []byte(n.BootID)); err != nil {
		return err
	}
	return n.Engine.Start(ctx)
}

func (n *Node) Heartbeat(ctx context.Context, version uint64) error {
	payload := []byte(fmt.Sprintf(`{"node_id":%q,"boot_id":%q,"version":%d}`, n.NodeID, n.BootID, version))
	return n.Store.AppendEvent(ctx, fmt.Sprintf("heartbeat-%s-%d-%d", n.BootID, version, time.Now().UnixNano()), "heartbeat", payload)
}

func (n *Node) Stop(ctx context.Context) error { return n.Engine.Stop(ctx) }
