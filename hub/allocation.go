package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
	"time"
	"vortex/agent/store"
)

// Allocation is the canonical lifecycle backend_allocation projection. It is
// funded and persisted by PostgreSQL before transport. It is never accepted
// from a Node or from an AuthorizationUpdate.
type Allocation struct {
	AllocationID      string                   `json:"allocation_id"`
	DelegationID      string                   `json:"delegation_id"`
	HubID             string                   `json:"hub_id"`
	Region            string                   `json:"region_id"`
	NodeID            string                   `json:"node_id"`
	BootID            string                   `json:"boot_id"`
	SourceID          string                   `json:"source_id"`
	RuntimeSourceID   string                   `json:"runtime_source_id"`
	ShareSourceID     string                   `json:"share_source_id"`
	SnapshotID        string                   `json:"billing_snapshot_id"`
	RuleID            string                   `json:"rule_id"`
	RuleRevision      uint64                   `json:"rule_revision"`
	PathGeneration    uint64                   `json:"path_generation"`
	FencingFloor      uint64                   `json:"fencing_generation_floor"`
	SignerEpoch       uint64                   `json:"signer_epoch"`
	RawBytes          uint64                   `json:"raw_bytes"`
	RateBPS           uint64                   `json:"rate_bps"`
	Connections       uint64                   `json:"connections"`
	ValidFrom         time.Time                `json:"valid_from"`
	ExpiresAt         time.Time                `json:"expires_at"`
	HardDeadline      time.Time                `json:"hard_deadline"`
	BackendHeartbeat  time.Time                `json:"backend_heartbeat_at"`
	AutonomyDeadline  time.Time                `json:"autonomy_deadline"`
	ShareReferences   []store.ShareReference   `json:"share_references"`
	PolicyTransitions []store.PolicyTransition `json:"policy_transitions"`
}

var ErrAllocation = errors.New("invalid funded backend allocation")

func DecodeAllocation(raw []byte) (Allocation, error) {
	var a Allocation
	if len(raw) == 0 || len(raw) > 1<<20 {
		return a, ErrAllocation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return Allocation{}, ErrAllocation
	}
	// Producer canonicalization also rejects omitted/null required fields,
	// duplicate keys, trailing values and alternate number encodings.
	canonical, e := json.Marshal(a)
	if e != nil || !bytes.Equal(canonical, raw) {
		return Allocation{}, ErrAllocation
	}
	return a, a.Validate()
}
func (a Allocation) Validate() error {
	for _, id := range []string{a.AllocationID, a.DelegationID, a.HubID, a.NodeID, a.BootID, a.SourceID, a.RuntimeSourceID, a.ShareSourceID, a.SnapshotID} {
		parsed, e := uuid.Parse(id)
		if e != nil || parsed.String() != id {
			return ErrAllocation
		}
	}
	if a.Region == "" || len(a.Region) > 128 || a.RuleID == "" || len(a.RuleID) > 128 || a.SourceID == a.RuntimeSourceID || a.SourceID == a.ShareSourceID || a.RuntimeSourceID == a.ShareSourceID {
		return ErrAllocation
	}
	for _, n := range []uint64{a.RuleRevision, a.PathGeneration, a.FencingFloor, a.SignerEpoch, a.RawBytes} {
		if n == 0 || n > math.MaxInt64 {
			return ErrAllocation
		}
	}
	if a.RateBPS == 0 || a.RateBPS > 1_000_000_000_000 || a.Connections == 0 || a.Connections > 1_000_000 || a.ValidFrom.IsZero() || !a.ExpiresAt.After(a.ValidFrom) || a.ExpiresAt.After(a.HardDeadline) || a.HardDeadline.After(a.AutonomyDeadline) || a.BackendHeartbeat.IsZero() || a.ValidFrom.Before(a.BackendHeartbeat) || !a.AutonomyDeadline.After(a.BackendHeartbeat) || a.AutonomyDeadline.After(a.BackendHeartbeat.Add(12*time.Hour)) {
		return ErrAllocation
	}
	// Ordinary current-policy producer only. Policy transitions use their own
	// reviewed producer; a nonempty unsupported transition cannot grant rights.
	if len(a.ShareReferences) != 3 || a.PolicyTransitions == nil || len(a.PolicyTransitions) != 0 {
		return ErrAllocation
	}
	sources := map[string]bool{a.SourceID: false, a.RuntimeSourceID: false, a.ShareSourceID: false}
	ids := map[string]bool{}
	for _, r := range a.ShareReferences {
		seen, ok := sources[r.SourceID]
		if !ok || seen || ids[r.ID] || store.ValidateShareReference(r) != nil || r.ParentID != "" || r.ParentRevision != 0 || r.NodeID != a.NodeID || r.IssuerID != a.HubID || r.SignerEpoch != a.SignerEpoch || r.RuleID != a.RuleID || r.RuleRevision != a.RuleRevision || r.PathGeneration != a.PathGeneration || r.RateBPS < a.RateBPS || r.Connections < a.Connections || a.ValidFrom.Before(r.NotBefore) || a.ExpiresAt.After(r.ExpiresAt) || a.HardDeadline.After(r.HardDeadline) {
			return ErrAllocation
		}
		sources[r.SourceID] = true
		ids[r.ID] = true
	}
	return nil
}
func (a Allocation) Delegation() store.HubDelegation {
	return store.HubDelegation{ID: a.DelegationID, HubID: a.HubID, SourceID: a.SourceID, RuntimeSourceID: a.RuntimeSourceID, ShareSourceID: a.ShareSourceID, NodeID: a.NodeID, BootID: a.BootID, RuleID: a.RuleID, BillingSnapshotID: a.SnapshotID, SignerEpoch: a.SignerEpoch, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, FencingFloor: a.FencingFloor, RawBytes: a.RawBytes, RateBPS: a.RateBPS, Connections: a.Connections, ValidFrom: a.ValidFrom, ExpiresAt: a.ExpiresAt, HardDeadline: a.HardDeadline, BackendHeartbeat: a.BackendHeartbeat, AutonomyDeadline: a.AutonomyDeadline, ShareReferences: a.ShareReferences}
}
