package agent

import (
	"context"
	"fmt"

	engineaccounting "vortex/engine/accounting"
	engineruntime "vortex/engine/runtime"
	"vortex/engine/runtime/config"
)

// RuntimeEngine adapts the Engine runtime lifecycle to the Agent process.
// Configuration is loaded before Start so malformed or unauthorized runtime
// state cannot be acknowledged as an Agent heartbeat.
type RuntimeEngine struct {
	ConfigPath string
	runtime    *engineruntime.Runtime
	scope      RuntimeScope
}

func NewRuntimeEngine(configPath, nodeID string) (*RuntimeEngine, error) {
	if configPath == "" {
		return nil, fmt.Errorf("agent: engine config path is required")
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	if cfg.NodeID != nodeID {
		return nil, fmt.Errorf("agent: engine node identity does not match agent")
	}
	rt, err := engineruntime.New(cfg)
	if err != nil {
		return nil, err
	}
	return &RuntimeEngine{ConfigPath: configPath, runtime: rt, scope: RuntimeScope{RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration}}, nil
}

func (e *RuntimeEngine) Start(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return fmt.Errorf("agent: runtime engine is not configured")
	}
	return e.runtime.Start(ctx)
}

// SetAccountingGate wires the Agent-owned durable grant gate before the Engine
// listener starts. It cannot replace policy after traffic admission begins.
func (e *RuntimeEngine) SetAccountingGate(gate engineaccounting.Gate) error {
	if e == nil || e.runtime == nil {
		return fmt.Errorf("agent: runtime engine is not configured")
	}
	return e.runtime.SetAccountingGate(gate)
}

func (e *RuntimeEngine) Stop(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return nil
	}
	return e.runtime.Stop(ctx)
}

func (e *RuntimeEngine) Done() <-chan struct{} {
	if e == nil || e.runtime == nil {
		return nil
	}
	return e.runtime.Done()
}

func (e *RuntimeEngine) Err() error {
	if e == nil || e.runtime == nil {
		return nil
	}
	return e.runtime.Err()
}

// EnableExperimentalFundingBlocks selects the durable block allocator for this
// Node's configured runtime, before Start. Scope comes from the loaded Engine
// configuration, never from an independent caller-supplied authority identity.
// It grants no credit; authenticated persisted authority is still mandatory.
func (n *Node) EnableExperimentalFundingBlocks(blockBytes uint64) error {
	if n == nil {
		return fmt.Errorf("agent: node is required")
	}
	e, ok := n.Engine.(*RuntimeEngine)
	if !ok || e == nil {
		return fmt.Errorf("agent: funding blocks require a runtime engine")
	}
	gate, err := n.NewExperimentalFundingBlockGate(e.scope, n.TrustedTime, blockBytes)
	if err != nil {
		return err
	}
	if err := e.SetAccountingGate(gate); err != nil {
		return err
	}
	return n.Store.EnableMonotonicRuntimeRate(n.BootID)
}
