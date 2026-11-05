package agent

import (
	"context"
	"fmt"

	engineruntime "vortex/engine/runtime"
	"vortex/engine/runtime/config"
)

// RuntimeEngine adapts the Engine runtime lifecycle to the Agent process.
// Configuration is loaded before Start so malformed or unauthorized runtime
// state cannot be acknowledged as an Agent heartbeat.
type RuntimeEngine struct {
	ConfigPath string
	runtime    *engineruntime.Runtime
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
	return &RuntimeEngine{ConfigPath: configPath, runtime: rt}, nil
}

func (e *RuntimeEngine) Start(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return fmt.Errorf("agent: runtime engine is not configured")
	}
	return e.runtime.Start(ctx)
}

func (e *RuntimeEngine) Stop(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return nil
	}
	return e.runtime.Stop(ctx)
}
