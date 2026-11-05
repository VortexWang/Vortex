package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	NodeID string
	BootID string
	Store  *store.Store
	Engine Engine
}

func New(nodeID string, s *store.Store, e Engine) (*Node, error) {
	if nodeID == "" || s == nil {
		return nil, fmt.Errorf("agent: node id and store are required")
	}
	if e == nil {
		e = NoopEngine{}
	}
	var boot [16]byte
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, fmt.Errorf("agent: boot id: %w", err)
	}
	return &Node{NodeID: nodeID, BootID: hex.EncodeToString(boot[:]), Store: s, Engine: e}, nil
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
