package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"vortex/agent/store"
	"vortex/engine/apply"
	"vortex/engine/runtime/config"
)

var ErrActivationPending = errors.New("agent: prepared configuration awaits boot-local authority")

// RuntimeApplier binds transaction completion to the configured Engine, rather
// than acknowledging a stand-alone transaction model. The current runtime
// supports one static destination. Unsupported changes are rejected explicitly.
type RuntimeApplier struct {
	EngineApplier
	node     *Node
	engine   *RuntimeEngine
	mu       sync.Mutex
	prepared map[[16]byte]bool
}

func (n *Node) RuntimeApplier() (*RuntimeApplier, error) {
	e, ok := n.Engine.(*RuntimeEngine)
	if !ok {
		return nil, fmt.Errorf("agent: runtime execution boundary unavailable")
	}
	e.activationRequired.Store(true)
	return &RuntimeApplier{EngineApplier: EngineApplier{Manager: apply.NewManager()}, node: n, engine: e, prepared: map[[16]byte]bool{}}, nil
}

func (a *RuntimeApplier) Query(id [16]byte) (apply.Transaction, bool) {
	result, ok := a.EngineApplier.Query(id)
	if !ok {
		return result, false
	}
	select {
	case <-a.engine.Done():
		result.State = apply.Released
	default:
	}
	return result, true
}

func (a *RuntimeApplier) PrepareConfig(id [16]byte, generation uint64, payload []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.engine.matchesControlConfig(payload); err != nil {
		return err
	}
	low, high, trusted := a.node.TrustedTime()
	scope := a.engine.scope
	if err := a.node.Store.HoldRuntimeShares(context.Background(), hex.EncodeToString(id[:]), store.AuthorizationUse{NodeID: a.node.NodeID, BootID: a.node.BootID, RuleID: scope.RuleID, RuleRevision: scope.RuleRevision, PathGeneration: scope.PathGeneration, FencingGeneration: generation, Earliest: low, Latest: high, TrustedTime: trusted}); err != nil {
		if errors.Is(err, store.ErrAuthorizationDenied) || errors.Is(err, store.ErrAuthorityTimeUncertain) {
			return ErrActivationPending
		}
		return err
	}
	if _, err := a.EngineApplier.Prepare(id, generation); err != nil {
		return err
	}
	a.prepared[id] = true
	return nil
}
func (a *RuntimeApplier) Prepare(id [16]byte, generation uint64) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.prepared[id] {
		return nil, ErrConfigConflict
	}
	return a.EngineApplier.Prepare(id, generation)
}
func (a *RuntimeApplier) Commit(id [16]byte, unknown bool) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tx, ok := a.EngineApplier.Query(id)
	if !ok {
		return nil, ErrConfigConflict
	}
	low, high, trusted := a.node.TrustedTime()
	scope := a.engine.scope
	if err := a.node.Store.CheckRuntimeReady(context.Background(), store.AuthorizationUse{NodeID: a.node.NodeID, BootID: a.node.BootID, RuleID: scope.RuleID, RuleRevision: scope.RuleRevision, PathGeneration: scope.PathGeneration, FencingGeneration: tx.Generation, Earliest: low, Latest: high, TrustedTime: trusted}); err != nil {
		if errors.Is(err, store.ErrAuthorizationDenied) || errors.Is(err, store.ErrAuthorityTimeUncertain) {
			return nil, ErrActivationPending
		}
		return nil, err
	}
	if err := a.node.Store.HoldRuntimeShares(context.Background(), hex.EncodeToString(id[:]), store.AuthorizationUse{NodeID: a.node.NodeID, BootID: a.node.BootID, RuleID: scope.RuleID, RuleRevision: scope.RuleRevision, PathGeneration: scope.PathGeneration, FencingGeneration: tx.Generation, Earliest: low, Latest: high, TrustedTime: trusted}); err != nil {
		return nil, err
	}
	if a.engine.runtime.Addr() == nil {
		return nil, ErrActivationPending
	}
	select {
	case <-a.engine.Done():
		return nil, ErrConfigConflict
	default:
	}
	result, err := a.EngineApplier.Commit(id, unknown)
	if err == nil {
		a.engine.activeFence.Store(tx.Generation)
		a.engine.activated.Store(true)
	}
	return result, err
}

// Stop proves shutdown using the actual runtime Done boundary, then disables
// admission. It does not infer termination from a new boot ID or timeout.
func (a *RuntimeApplier) Stop(ctx context.Context) error {
	if err := a.engine.Stop(ctx); err != nil {
		return err
	}
	a.engine.activated.Store(false)
	return nil
}

func (e *RuntimeEngine) matchesControlConfig(raw []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	allowed := map[string]bool{"version": true, "node_id": true, "listeners": true, "rules": true, "revocations": true}
	for key := range object {
		if !allowed[key] {
			return fmt.Errorf("unsupported_config: runtime option %s is not implemented", key)
		}
	}
	var rules []map[string]json.RawMessage
	if err := json.Unmarshal(object["rules"], &rules); err != nil {
		return err
	}
	fields := map[string]bool{"rule_id": true, "revision": true, "path_generation": true, "role": true, "hop_index": true, "mode": true, "targets": true, "allowed_upstream_node_ids": true, "link_mode": true}
	for _, rule := range rules {
		for key := range rule {
			if !fields[key] {
				return fmt.Errorf("unsupported_config: runtime rule option %s is not implemented", key)
			}
		}
	}
	var c struct {
		NodeID    string `json:"node_id"`
		Listeners []struct {
			Kind, Address string
			Port          int
		} `json:"listeners"`
		Rules []struct {
			ID         string   `json:"rule_id"`
			Revision   uint64   `json:"revision"`
			Generation uint64   `json:"path_generation"`
			Role       string   `json:"role"`
			Hop        uint16   `json:"hop_index"`
			Mode       string   `json:"mode"`
			Targets    []string `json:"targets"`
			Upstream   []string `json:"allowed_upstream_node_ids"`
			LinkMode   string   `json:"link_mode"`
		} `json:"rules"`
		Revocations []string `json:"revocations"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	cfg := e.config
	if c.NodeID != cfg.NodeID || len(c.Rules) != 1 || len(c.Listeners) != 1 || len(c.Revocations) != 0 {
		return fmt.Errorf("unsupported_config: runtime requires one unchanged destination rule and listener")
	}
	r := c.Rules[0]
	if cfg.Role != config.RoleDestination || r.ID != cfg.RuleID || r.Revision != cfg.RuleRevision || r.Generation != cfg.PathGeneration || r.Role != "exit" || r.Hop != cfg.HopIndex || r.Mode != "tunnel" || len(r.Targets) != 1 || r.Targets[0] != cfg.Target || len(r.Upstream) != 1 || r.Upstream[0] != cfg.PeerNodeID {
		return fmt.Errorf("unsupported_config: rule differs from configured runtime")
	}
	address := cfg.Listen
	if e.runtime.Addr() != nil {
		address = e.runtime.Addr().String()
	}
	l := c.Listeners[0]
	if l.Kind != "link" || net.JoinHostPort(l.Address, strconv.Itoa(l.Port)) != address {
		return fmt.Errorf("unsupported_config: listener differs from configured runtime")
	}
	if r.LinkMode != "" && r.LinkMode != "auto" && string(cfg.TransportMode) != r.LinkMode {
		return fmt.Errorf("unsupported_config: link mode differs from runtime")
	}
	return nil
}
