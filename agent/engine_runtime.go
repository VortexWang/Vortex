package agent

import (
	"context"
	"fmt"
	"sync/atomic"

	engineaccounting "vortex/engine/accounting"
	engineruntime "vortex/engine/runtime"
	"vortex/engine/runtime/config"
)

// RuntimeEngine adapts the Engine runtime lifecycle to the Agent process.
// Configuration is loaded before Start so malformed or unauthorized runtime
// state cannot be acknowledged as an Agent heartbeat.
type RuntimeEngine struct {
	config             config.Config
	activationRequired atomic.Bool
	activated          atomic.Bool
	activeFence        atomic.Uint64
	ConfigPath         string
	runtime            *engineruntime.Runtime
	scope              RuntimeScope
}

func NewRuntimeEngine(configPath, nodeID string) (*RuntimeEngine, error) {
	return newRuntimeEngine(configPath, nodeID, false)
}

// NewManagedRuntimeEngine is the node composition entry point: identity comes
// from the durable Agent manager, never runtime file aliases.
func NewManagedRuntimeEngine(configPath, nodeID string) (*RuntimeEngine, error) {
	return newRuntimeEngine(configPath, nodeID, true)
}
func newRuntimeEngine(configPath, nodeID string, managed bool) (*RuntimeEngine, error) {
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
	var rt *engineruntime.Runtime
	if managed {
		rt, err = engineruntime.NewForAgent(cfg)
	} else {
		rt, err = engineruntime.New(cfg)
	}
	if err != nil {
		return nil, err
	}
	return &RuntimeEngine{config: cfg, ConfigPath: configPath, runtime: rt, scope: RuntimeScope{RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration}}, nil
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
