package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
	"vortex/hub"
)

// NodeBootRecoveryInput is a platform approval for a particular new process.
// It cannot reset generations, forgive debt or attest old process termination.
type NodeBootRecoveryInput struct {
	ID             string `json:"recovery_id"`
	NodeID         string `json:"node_id"`
	FromBootID     string `json:"from_boot_id"`
	ToBootID       string `json:"to_boot_id"`
	BackendID      string `json:"backend_id"`
	MinimumFencing int64  `json:"minimum_fencing_generation"`
	Reason         string `json:"reason"`
}

type nodeBootRecoveryProof struct {
	Version        int                                       `json:"version"`
	Request        NodeBootRecoveryInput                     `json:"request"`
	Earliest       time.Time                                 `json:"earliest"`
	Latest         time.Time                                 `json:"latest"`
	SafeAfter      time.Time                                 `json:"safe_after"`
	Shares         []generated.ShareGeneration               `json:"shares"`
	Allocations    []hubColdAllocationProof                  `json:"allocations"`
	Leases         []generated.LeaseExpectation              `json:"leases"`
	Configurations []generated.NodeRecoveryConfigurationsRow `json:"configurations"`
}

// PrepareNodeBootRecovery snapshots every tenant's old-boot funded authority.
// The current Node row is locked: old-boot producers cannot pass their current
// incarnation checks while this proof is built. Only an already confirmed pair
// of exact aggregate commands is supported; partial/unknown CAS stays held.
func (s ShareService) PrepareNodeBootRecovery(ctx context.Context, scope TenantScope, in NodeBootRecoveryInput, request string) (generated.NodeBootRecovery, error) {
	var out generated.NodeBootRecovery
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	for _, id := range []string{in.ID, in.NodeID, in.FromBootID, in.ToBootID, in.BackendID} {
		if !validID(id) {
			return out, ErrShareConflict
		}
	}
	if in.FromBootID == in.ToBootID || in.MinimumFencing < 1 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 256 {
		return out, ErrShareConflict
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "node.prepare_boot_recovery", ResourceID: in.NodeID, RequestID: request, Reason: in.Reason}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityWrite(ctx, in.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		prior, e := q.NodeBootRecoveryByOldBoot(ctx, generated.NodeBootRecoveryByOldBootParams{TenantID: scope.TenantID(), NodeID: in.NodeID, FromBootID: in.FromBootID})
		if e == nil {
			_, p, e := validateNodeBootRecovery(prior)
			if e != nil || p.Request != in {
				return ErrShareConflict
			}
			out = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		current, e := q.LockNodeRecoveryIncarnation(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if current.BootID != in.ToBootID {
			return ErrShareUnknown
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		proof := nodeBootRecoveryProof{Version: 1, Request: in, Earliest: lo, Latest: hi, Allocations: []hubColdAllocationProof{}}
		advance := func(end time.Time) {
			if end.After(proof.SafeAfter) {
				proof.SafeAfter = end.UTC()
			}
		}
		proof.Shares, e = q.NodeRecoveryShares(ctx, generated.NodeRecoverySharesParams{NodeID: in.NodeID, BootID: in.FromBootID})
		if e != nil {
			return e
		}
		for _, g := range proof.Shares {
			for _, t := range []time.Time{g.OwnershipUntil.Time, g.PolicyUntil.Time, g.AutonomyUntil.Time} {
				advance(t)
			}
		}
		allocations, e := q.NodeRecoveryAllocations(ctx, generated.NodeRecoveryAllocationsParams{NodeID: in.NodeID, BootID: in.FromBootID})
		if e != nil {
			return e
		}
		for _, a := range allocations {
			hash := sha256.Sum256(a.Payload)
			original, e := hub.DecodeAllocation(a.Payload)
			if e != nil || !bytes.Equal(hash[:], a.PayloadDigest) || original.NodeID != in.NodeID || original.BootID != in.FromBootID || original.AllocationID != a.AllocationID {
				return ErrShareConflict
			}
			advance(original.AutonomyDeadline)
			proof.Allocations = append(proof.Allocations, hubColdAllocationProof{a.TenantID, a.AllocationID, a.ReservationID, hex.EncodeToString(hash[:])})
		}
		proof.Leases, e = q.NodeRecoveryLeases(ctx, generated.NodeRecoveryLeasesParams{NodeID: in.NodeID, BootID: in.FromBootID})
		if e != nil {
			return e
		}
		for _, l := range proof.Leases {
			for _, t := range []time.Time{l.RelativeDeadline.Time, l.OwnershipUntil.Time, l.PolicyUntil.Time, l.AutonomyUntil.Time} {
				advance(t)
			}
		}
		proof.Configurations, e = q.NodeRecoveryConfigurations(ctx, generated.NodeRecoveryConfigurationsParams{NodeID: in.NodeID, BootID: in.FromBootID})
		if e != nil {
			return e
		}
		// Configuration deadlines are included conservatively, not converted into
		// proof of Engine stop. Any installed runtime authority is bounded above by
		// the immutable source roots included in the allocation inventory.
		for _, c := range proof.Configurations {
			advance(c.Deadline.Time)
		}
		if proof.SafeAfter.IsZero() || lo.Before(proof.SafeAfter) {
			return ErrShareUnknown
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.FromBootID})
		if e != nil {
			return e
		}
		if fence.RenewalClosed || in.MinimumFencing <= fence.FencingGeneration {
			return ErrShareConflict
		}
		c := contracts.NodeBootRecovery{Version: 1, ID: in.ID, BackendID: in.BackendID, TenantID: scope.TenantID(), NodeID: in.NodeID, FromBootID: in.FromBootID, ToBootID: in.ToBootID, MinimumFencing: in.MinimumFencing, SafeAfter: proof.SafeAfter, IssuedAt: lo, ExpiresAt: hi.Add(4 * time.Minute), Previous: []contracts.ShareHandoff{}}
		for _, kind := range []string{"rate", "connections"} {
			chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind})
			if e != nil {
				return e
			}
			if chain.BootID != in.FromBootID || chain.HeadGeneration < 1 || chain.ActiveGeneration != chain.HeadGeneration || chain.PendingGeneration != 0 || chain.RetiredGeneration != 0 {
				return ErrShareUnknown
			}
			h, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind, Generation: chain.HeadGeneration})
			if e != nil {
				return e
			}
			p, e := contracts.DecodeShareHandoff(h.Command)
			if e != nil || h.State != "enforced" || contracts.SharePayloadDigest(h.Command) != hex.EncodeToString(h.CommandDigest) || p.BootID != in.FromBootID || p.TenantID != scope.TenantID() || p.NodeID != in.NodeID || p.Kind != kind || p.Generation != chain.HeadGeneration {
				return ErrShareUnknown
			}
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind, Generation: chain.HeadGeneration})
			if e != nil {
				return e
			}
			if !nodeRecoveryOriginalMatches(p, g) {
				return ErrShareUnknown
			}
			c.Previous = append(c.Previous, p)
		}
		proofRaw, e := json.Marshal(proof)
		if e != nil {
			return e
		}
		if len(proofRaw) > 4<<20 {
			return ErrShareUnknown
		}
		proofHash := sha256.Sum256(proofRaw)
		c.AuthorityProofSHA256 = hex.EncodeToString(proofHash[:])
		raw, e := json.Marshal(c)
		if e != nil {
			return e
		}
		if _, e = contracts.DecodeNodeBootRecovery(raw); e != nil {
			return e
		}
		hash := sha256.Sum256(raw)
		op, e := newDomainTask(ctx, q, scope, in.NodeID, "node.recover_boot", "resource_change", in.MinimumFencing)
		if e != nil {
			return e
		}
		if e = q.AppendNodeBootRecovery(ctx, generated.AppendNodeBootRecoveryParams{TenantID: scope.TenantID(), ID: in.ID, NodeID: in.NodeID, FromBootID: in.FromBootID, ToBootID: in.ToBootID, BackendID: in.BackendID, OperationID: op.ID, CreatedBy: scope.ActorID(), Proof: proofRaw, ProofDigest: proofHash[:], Payload: raw, PayloadDigest: hash[:]}); e != nil {
			return e
		}
		out, e = q.GetNodeBootRecovery(ctx, generated.GetNodeBootRecoveryParams{TenantID: scope.TenantID(), ID: in.ID})
		return e
	})
	if e != nil {
		return generated.NodeBootRecovery{}, e
	}
	return out, nil
}

func validateNodeBootRecovery(p generated.NodeBootRecovery) (contracts.NodeBootRecovery, nodeBootRecoveryProof, error) {
	c, e := contracts.DecodeNodeBootRecovery(p.Payload)
	var proof nodeBootRecoveryProof
	a, b := sha256.Sum256(p.Payload), sha256.Sum256(p.Proof)
	if e != nil || !bytes.Equal(a[:], p.PayloadDigest) || !bytes.Equal(b[:], p.ProofDigest) || c.AuthorityProofSHA256 != hex.EncodeToString(b[:]) || json.Unmarshal(p.Proof, &proof) != nil {
		return c, proof, ErrShareConflict
	}
	in := proof.Request
	if proof.Version != 1 || c.ID != p.ID || c.NodeID != p.NodeID || c.TenantID != p.TenantID || c.FromBootID != p.FromBootID || c.ToBootID != p.ToBootID || c.BackendID != p.BackendID || in.ID != c.ID || in.NodeID != c.NodeID || in.FromBootID != c.FromBootID || in.ToBootID != c.ToBootID || in.BackendID != c.BackendID || in.MinimumFencing != c.MinimumFencing || !proof.SafeAfter.Equal(c.SafeAfter) || proof.Earliest.Before(proof.SafeAfter) || proof.Latest.Before(proof.Earliest) {
		return c, proof, ErrShareConflict
	}
	return c, proof, nil
}

// RecordNodeBootRecovery accepts only a canonical receipt from the pinned
// current Node certificate. The retired floor is separate from active capacity:
// active=0, held=max(old,new) until the actual next CAS is acknowledged. No
// financial table, old execution receipt or configuration state changes here.
func (s ShareService) RecordNodeBootRecovery(ctx context.Context, scope TenantScope, raw []byte, request string) error {
	var r contracts.NodeBootRecoveryReceipt
	if len(raw) == 0 || len(raw) > 64<<10 || json.Unmarshal(raw, &r) != nil {
		return ErrStaleReceipt
	}
	fingerprint, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.boot_retired", ResourceID: r.ID, RequestID: request, Reason: "authenticated FULL zero-capacity retirement; preserve independent floors and all unknown occupation"}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityWrite(ctx, r.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		p, e := q.GetNodeBootRecovery(ctx, generated.GetNodeBootRecoveryParams{TenantID: scope.TenantID(), ID: r.ID})
		if e != nil {
			return e
		}
		c, _, e := validateNodeBootRecovery(p)
		if e != nil {
			return e
		}
		expected := contracts.NodeBootRecoveryReceipt{Version: 1, ID: c.ID, NodeID: c.NodeID, BootID: c.ToBootID, CommandSHA256: hex.EncodeToString(p.PayloadDigest), MinimumFencing: c.MinimumFencing, Retired: []contracts.RetiredNodeShareHead{}}
		for _, g := range c.Previous {
			expected.Retired = append(expected.Retired, contracts.RetiredNodeShareHead{Kind: g.Kind, Generation: g.Generation})
		}
		canonical, e := json.Marshal(expected)
		if e != nil {
			return e
		}
		if !bytes.Equal(canonical, raw) {
			return ErrStaleReceipt
		}
		prior, e := q.GetNodeBootRecoveryReceipt(ctx, generated.GetNodeBootRecoveryReceiptParams{TenantID: scope.TenantID(), RecoveryID: r.ID})
		if e == nil {
			hash := sha256.Sum256(prior.Payload)
			if !bytes.Equal(prior.Payload, raw) || !bytes.Equal(hash[:], prior.PayloadDigest) || !bytes.Equal(prior.PeerFingerprint, fingerprint) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		current, e := q.LockNodeRecoveryIncarnation(ctx, c.NodeID)
		if e != nil {
			return e
		}
		if current.BootID != c.ToBootID {
			return ErrStaleReceipt
		}
		if e = q.EnsureNodeFencing(ctx, generated.EnsureNodeFencingParams{TenantID: scope.TenantID(), NodeID: c.NodeID, BootID: c.ToBootID, FencingGeneration: c.MinimumFencing}); e != nil {
			return e
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: c.NodeID, BootID: c.ToBootID})
		if e != nil {
			return e
		}
		if fence.RenewalClosed || fence.FencingGeneration != c.MinimumFencing {
			return ErrShareConflict
		}
		for _, g := range c.Previous {
			n, e := q.RetireNodeShareChain(ctx, generated.RetireNodeShareChainParams{TenantID: scope.TenantID(), NodeID: c.NodeID, Kind: g.Kind, OldBoot: c.FromBootID, NewBoot: c.ToBootID, Generation: g.Generation})
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrShareUnknown
			}
		}
		oldFence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: c.NodeID, BootID: c.FromBootID})
		if e != nil {
			return e
		}
		if oldFence.RenewalClosed || oldFence.FencingGeneration >= c.MinimumFencing {
			return ErrShareUnknown
		}
		hash := sha256.Sum256(raw)
		n, e := q.CloseNodeBootFencing(ctx, generated.CloseNodeBootFencingParams{TenantID: scope.TenantID(), NodeID: c.NodeID, BootID: c.FromBootID, ClosureProof: hash[:]})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrShareUnknown
		}
		if e = q.CloseNodeBootRenewal(ctx, generated.CloseNodeBootRenewalParams{TenantID: scope.TenantID(), NodeID: c.NodeID, BootID: c.FromBootID, ClosedAt: timestamp(c.IssuedAt), ClosureProof: hash[:]}); e != nil {
			return e
		}
		if e = q.AppendNodeBootRecoveryReceipt(ctx, generated.AppendNodeBootRecoveryReceiptParams{TenantID: scope.TenantID(), RecoveryID: r.ID, Payload: raw, PayloadDigest: hash[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: p.OperationID}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, c.NodeID, "node.boot_retired", "resource_change", c.MinimumFencing)
	})
}

// The locally retained command must be the original immutable generation in
// every authority dimension; a matching generation number alone is insufficient.
func nodeRecoveryOriginalMatches(p contracts.ShareHandoff, g generated.ShareGeneration) bool {
	return p.TenantID == g.TenantID && p.ID == g.ID && p.NodeID == g.NodeID && p.BootID == g.BootID && p.Kind == g.Kind && p.Generation == g.Generation && p.Predecessor == g.Predecessor && p.SignerID == g.SignerID && p.SignerEpoch == g.SignerEpoch && p.SnapshotID == g.BillingSnapshotID && p.RuleID == g.RuleID && p.RuleRevision == g.RuleRevision && p.PathGeneration == g.PathGeneration && p.FencingGeneration == g.FencingGeneration && p.Amount == g.Amount && p.BurstBytes == g.BurstBytes && !p.IssuedAt.Before(g.CreatedAt.Time) && p.OwnershipUntil.Equal(g.OwnershipUntil.Time) && p.PolicyUntil.Equal(g.PolicyUntil.Time) && p.AutonomyUntil.Equal(g.AutonomyUntil.Time)
}
