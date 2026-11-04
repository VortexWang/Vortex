package relay

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/netip"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

func StaticPolicy(cfg config.Config) (vlink.OpenPolicy, error) {
	local, err := vlink.ParseNodeID(cfg.NodeID)
	if err != nil {
		return vlink.OpenPolicy{}, err
	}
	ruleID := [16]byte{}
	digest := sha256.Sum256([]byte(cfg.RuleID))
	copy(ruleID[:], digest[:16])
	snapshot := vlink.RuleSnapshot{
		RuleID:          ruleID,
		Revision:        cfg.RuleRevision,
		PathGeneration:  cfg.PathGeneration,
		HopIndex:        cfg.HopIndex,
		ExecutionPoint:  cfg.ExecutionPoint,
		Active:          true,
		AllowedNetworks: map[uint8]bool{1: true, 2: true},
		SelfHosted:      cfg.ExecutionPoint,
	}
	if cfg.QualificationOnly {
		snapshot.RuntimeAuthorized = true
		snapshot.SegmentAllowed = true
		snapshot.RateShareValid = true
		snapshot.ConnectionShareValid = true
		snapshot.BillingAuthorized = true
		snapshot.BillingSnapshotID = "qualification"
		snapshot.BillingCoefficient = 1
	}
	if cfg.SourceCreditBook != nil && cfg.PolicyWindow != nil {
		// These fields are enabled only by an explicit injected credit book and
		// policy window. Ordinary runtime configs remain deny-by-default.
		snapshot.RuntimeAuthorized = true
		snapshot.SegmentAllowed = true
		snapshot.RateShareValid = true
		snapshot.ConnectionShareValid = true
		snapshot.BillingAuthorized = true
		snapshot.BillingSnapshotID = cfg.PolicyWindow.OldSnapshot
		if active := cfg.SourceCreditBook.Active; active != "" {
			snapshot.BillingSnapshotID = active
		}
		if v, ok := cfg.SourceCreditBook.Snapshots[snapshot.BillingSnapshotID]; ok {
			snapshot.BillingCoefficient = v.Binding.CoefficientMilli
		}
	}
	if cfg.NextHopNodeID != "" {
		next, parseErr := vlink.ParseNodeID(cfg.NextHopNodeID)
		if parseErr != nil {
			return vlink.OpenPolicy{}, fmt.Errorf("relay: next_hop_node_id: %w", parseErr)
		}
		snapshot.NextHops = []vlink.NodeID{next}
		snapshot.AllowedPeers = map[vlink.NodeID]bool{next: true}
	}
	if cfg.PeerNodeID != "" {
		peer, parseErr := vlink.ParseNodeID(cfg.PeerNodeID)
		if parseErr != nil {
			return vlink.OpenPolicy{}, fmt.Errorf("relay: peer_node_id: %w", parseErr)
		}
		if snapshot.AllowedPeers == nil {
			snapshot.AllowedPeers = map[vlink.NodeID]bool{}
		}
		snapshot.AllowedPeers[peer] = true
	}
	if cfg.Target != "" {
		target, parseErr := netip.ParseAddrPort(cfg.Target)
		if parseErr != nil {
			return vlink.OpenPolicy{}, fmt.Errorf("relay: target: %w", parseErr)
		}
		snapshot.Target = vlink.LocalTarget{Host: target.Addr().String(), Port: target.Port()}
	}
	if cfg.ExecutionPoint {
		// The execution point is the terminal hop. A self next-hop marker lets
		// the shared OpenPolicy return its local target decision without adding a
		// separate authorization path.
		snapshot.NextHops = []vlink.NodeID{local}
	}
	if snapshot.AllowedPeers == nil {
		snapshot.AllowedPeers = map[vlink.NodeID]bool{}
	}
	// Peer identity is checked by TLS HELLO; path membership is configured by
	// NextHopNodeID and the local rule snapshot.
	return vlink.OpenPolicy{LocalNode: local, Audit: func(event vlink.OpenAuditEvent) {
		slog.Warn("vlink OPEN rejected", "node_id", local.String(), "peer_id", event.PeerID.String(), "code", event.Code, "correlation_id", fmt.Sprintf("%x", event.CorrelationID))
	}, Lookup: func(context.Context, [16]byte) (vlink.RuleSnapshot, bool) {
		return snapshot, true
	}, SessionBillingSnapshotID: snapshot.BillingSnapshotID, SessionBillingCoefficient: snapshot.BillingCoefficient}, nil
}
