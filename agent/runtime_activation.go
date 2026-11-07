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
	engineaccounting "vortex/engine/accounting"
	"vortex/engine/apply"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	ruleruntime "vortex/engine/runtime/rule"
	"vortex/engine/vlink"
)

var ErrActivationPending = errors.New("agent: prepared configuration awaits boot-local authority")

// RuntimeApplier binds transaction completion to the configured Engine, rather
// than acknowledging a stand-alone transaction model. The current runtime
// supports one static destination. Unsupported changes are rejected explicitly.
type RuntimeApplier struct {
	EngineApplier
	node             *Node
	engine           *RuntimeEngine
	mu               sync.Mutex
	prepared         map[[16]byte]bool
	rules            *ruleruntime.Manager
	ruleTransactions map[[16]byte]ruleruntime.Artifact
}

func (n *Node) RuntimeApplier() (*RuntimeApplier, error) {
	e, ok := n.Engine.(*RuntimeEngine)
	if !ok {
		return nil, fmt.Errorf("agent: runtime execution boundary unavailable")
	}
	e.activationRequired.Store(true)
	a := &RuntimeApplier{EngineApplier: EngineApplier{Manager: apply.NewManager()}, node: n, engine: e, prepared: map[[16]byte]bool{}, ruleTransactions: map[[16]byte]ruleruntime.Artifact{}}
	a.rules = ruleruntime.New(func(artifact ruleruntime.Artifact) (engineaccounting.Gate, error) {
		return &ruleAuthorityGate{node: n, scope: RuntimeScope{RuleID: artifact.RuleID, RuleRevision: uint64(artifact.RuleRevision), PathGeneration: uint64(artifact.PathGeneration)}, fence: uint64(artifact.FencingGeneration), snapshot: artifact.BillingSnapshotID}, nil
	})
	if err := a.rules.SetLinkFactory(func(artifact ruleruntime.Artifact, peer ruleruntime.Member) (*link.Supervisor, error) {
		if e.credentials == nil {
			return nil, fmt.Errorf("agent: managed VLink credentials required")
		}
		cfg := e.config
		cfg.Peer = net.JoinHostPort(peer.PublicAddress, strconv.Itoa(int(peer.LinkPort)))
		cfg.NextHopNodeID = peer.ID
		cfg.PeerNodeID = ""
		cfg.PathGeneration = uint64(artifact.PathGeneration)
		supervisor, err := link.NewManaged(cfg)
		if err != nil {
			return nil, err
		}
		boot, err := vlink.ParseNodeID(n.BootID)
		if err != nil {
			return nil, err
		}
		if err = supervisor.SetIdentityProvider(e.credentials.RuntimeIdentity, vlink.BootID(boot)); err != nil {
			return nil, err
		}
		return supervisor, nil
	}); err != nil {
		return nil, err
	}
	if err := e.runtime.SetRuleManager(a.rules); err != nil {
		return nil, err
	}
	e.rules = a.rules
	return a, nil
}

func (a *RuntimeApplier) Query(id [16]byte) (apply.Transaction, bool) {
	a.mu.Lock()
	_, phased := a.ruleTransactions[id]
	a.mu.Unlock()
	if phased {
		return a.rules.Query(id)
	}
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
	if artifact, ok := a.ruleTransactions[id]; ok {
		low, high, trusted := a.node.TrustedTime()
		if artifact.Intent.Rule.Enabled {
			if err := a.node.Store.CheckRuntimeReady(context.Background(), store.AuthorizationUse{NodeID: a.node.NodeID, BootID: a.node.BootID, RuleID: artifact.RuleID, RuleRevision: uint64(artifact.RuleRevision), PathGeneration: uint64(artifact.PathGeneration), FencingGeneration: uint64(artifact.FencingGeneration), BillingSnapshotID: artifact.BillingSnapshotID, Earliest: low, Latest: high, TrustedTime: trusted}); err != nil {
				return nil, err
			}
		}
		if unknown {
			return nil, ErrActivationUnknown
		}
		if err := a.rules.Commit(id); err != nil {
			return nil, err
		}
		tx, _ := a.rules.Query(id)
		return tx, nil
	}
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
	if a.rules != nil {
		if err := a.rules.Stop(ctx); err != nil {
			return err
		}
	}
	if err := a.engine.Stop(ctx); err != nil {
		return err
	}
	a.engine.activated.Store(false)
	return nil
}

func (a *RuntimeApplier) ValidateRuleArtifact(raw []byte, node string, revision, generation uint64) error {
	artifact, err := ruleruntime.Decode(raw)
	if err != nil {
		return err
	}
	if artifact.NodeID != node || artifact.BootID != a.node.BootID || uint64(artifact.RuleRevision) != revision || uint64(artifact.FencingGeneration) != generation {
		return fmt.Errorf("identity_mismatch: artifact binding")
	}
	return nil
}
func (a *RuntimeApplier) PrepareRuleArtifact(id [16]byte, generation uint64, raw []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	artifact, err := ruleruntime.Decode(raw)
	if err != nil {
		return err
	}
	if artifact.NodeID != a.node.NodeID || artifact.BootID != a.node.BootID || uint64(artifact.FencingGeneration) != generation {
		return ErrConfigConflict
	}
	if artifact.Intent.Rule.Enabled {
		low, high, trusted := a.node.TrustedTime()
		if err = a.node.Store.HoldRuntimeShares(context.Background(), hex.EncodeToString(id[:]), store.AuthorizationUse{NodeID: a.node.NodeID, BootID: a.node.BootID, RuleID: artifact.RuleID, RuleRevision: uint64(artifact.RuleRevision), PathGeneration: uint64(artifact.PathGeneration), FencingGeneration: generation, BillingSnapshotID: artifact.BillingSnapshotID, Earliest: low, Latest: high, TrustedTime: trusted}); err != nil {
			return err
		}
	}
	if err = a.rules.Prepare(id, artifact); err != nil {
		return err
	}
	a.ruleTransactions[id] = artifact
	return nil
}
func (a *RuntimeApplier) Abort(id [16]byte) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.ruleTransactions[id]; ok {
		err := a.rules.Abort(id)
		tx, _ := a.rules.Query(id)
		return tx, err
	}
	return a.EngineApplier.Abort(id)
}

// ReleaseRule is scoped to the exact prepared/active transaction. Engine proves
// all sockets and flows for this rule stopped before Agent persists a receipt.
func (a *RuntimeApplier) ReleaseRule(ctx context.Context, id [16]byte) error {
	return a.rules.Release(ctx, id)
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
