package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// HubFundingInput is an online backend decision, not a Node request. The raw
// budget is reserved once in its original traffic source before any delivery.
type HubFundingInput struct {
	// Internal scheduler barrier, rechecked under the tenant lock. The public
	// immutable nonce binds this complete finalized predecessor inventory.
	runtimeHistorySHA256                                                  string
	NodeID, BootID, HubID, HubBootID, Region, SourceID, SnapshotID, Nonce string
	SignerEpoch, PathGeneration, FencingFloor, RawBytes                   int64
	ExpiresAt                                                             time.Time
}

func (s ShareService) PrepareHubAllocation(ctx context.Context, scope TenantScope, in HubFundingInput, request string) (hub.Allocation, error) {
	var out hub.Allocation
	if in.RawBytes < 1 || in.Nonce == "" || len(in.Nonce) > 128 || in.SignerEpoch < 1 || in.PathGeneration < 1 || in.FencingFloor < 1 {
		return out, ErrShareConflict
	}
	for _, id := range []string{in.NodeID, in.BootID, in.HubID, in.HubBootID, in.SourceID, in.SnapshotID} {
		if !validID(id) {
			return out, ErrShareConflict
		}
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return out, e
	}
	digest := sha256.Sum256(raw)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.fund_allocation", ResourceID: in.NodeID, RequestID: request, Reason: "one source-funded parent hold with immutable Hub allocation and task; no transport before commit"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+in.HubID); e != nil {
			return e
		}
		if e := q.LockNodeAuthorityRead(ctx, in.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.FindHubAllocation(ctx, generated.FindHubAllocationParams{TenantID: scope.TenantID(), RequestKey: in.Nonce})
		if e == nil {
			if !bytes.Equal(digest[:], old.RequestDigest) {
				return ErrShareConflict
			}
			out, e = hub.DecodeAllocation(old.Payload)
			return e
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if in.runtimeHistorySHA256 != "" {
			open, history, _, e := runtimeFundingHistory(ctx, q, scope.TenantID(), in.NodeID, in.BootID, in.SnapshotID, in.PathGeneration, in.FencingFloor)
			if e != nil {
				return e
			}
			if open || history != in.runtimeHistorySHA256 {
				return ErrShareUnknown
			}
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		if !in.ExpiresAt.After(hi) {
			return ErrLeaseBoundary
		}
		// Node assignment and both independently enforced aggregate kinds are checked
		// under the tenant serialization lock. Pending and unknown are insufficient.
		assigned, e := q.UsageNodeHub(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if assigned != in.HubID {
			return ErrScope
		}
		node, e := q.GetNodeIncarnation(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != in.BootID {
			return ErrShareConflict
		}
		boundary, e := q.HubSignerBoundary(ctx, in.HubID)
		if e != nil {
			return e
		}
		if boundary.SignerEpoch != in.SignerEpoch {
			return ErrShareUnknown
		}
		if !hi.Before(boundary.AutonomyUntil.Time) || in.ExpiresAt.After(boundary.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		incarnation, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: in.HubID, SignerEpoch: in.SignerEpoch})
		if e != nil {
			return e
		}
		if incarnation.BootID != in.HubBootID {
			return ErrShareUnknown
		}
		signer, e := q.GetSignerAuthority(ctx, generated.GetSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: in.HubID, SignerEpoch: in.SignerEpoch})
		if e != nil {
			return e
		}
		if !hi.Before(signer.AutonomyUntil.Time) || in.ExpiresAt.After(signer.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		limits, b, e := shareEligibility(ctx, q, scope, PrepareShareInput{NodeID: in.NodeID, BootID: in.BootID, SnapshotID: in.SnapshotID, PathGeneration: in.PathGeneration, PolicyUntil: in.ExpiresAt}, hi)
		if e != nil {
			return e
		}
		prepared, e := q.PreparedShareArtifacts(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		ready := false
		var ruleRate, ruleConnections int64
		for _, p := range prepared {
			if p.NodeID == in.NodeID && p.BootID == in.BootID && p.BillingSnapshotID == in.SnapshotID && p.PathGeneration == in.PathGeneration && p.FencingGeneration == in.FencingFloor {
				epoch, e := effectiveRuntimeEpochTx(ctx, q, scope.TenantID(), p.NodeID, p.BootID, p.SignerEpoch, p.PathGeneration, p.FencingGeneration)
				if e != nil {
					return e
				}
				if epoch != in.SignerEpoch {
					continue
				}
				if _, e = repository.CommittedArtifact(ctx, q, scope.TenantID(), repository.ArtifactBinding{RuleID: p.RuleID, NodeID: p.NodeID, Digest: p.Digest, Revision: p.RuleRevision, FencingGeneration: p.FencingGeneration}); e != nil {
					return e
				}
				pinned, e := q.GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: scope.TenantID(), RuleID: p.RuleID, RuleRevision: p.RuleRevision, InputDigest: p.InputDigest})
				if e != nil {
					return e
				}
				var previous compiler.CompileInput
				if e = json.Unmarshal(pinned.Canonical, &previous); e != nil {
					return e
				}
				current, e := lockedCompileInput(ctx, q, scope, p.RuleID, p.RuleRevision, previous.Billing, p.SignerEpoch, p.PathGeneration, p.FencingGeneration)
				if e != nil {
					return e
				}
				current.ValidAt = previous.ValidAt
				rebuilt := compiler.Compile(ctx, current)
				if len(rebuilt.Diagnostics) != 0 || rebuilt.InputDigest != p.InputDigest {
					continue
				}
				ready = true
				ruleRate, ruleConnections = previous.Rule.RateLimit, previous.Rule.ConnectionLimit
			}
		}
		if !ready {
			return ErrShareUnknown
		}
		until, e := q.SnapshotEffectiveUntil(ctx, generated.SnapshotEffectiveUntilParams{TenantID: scope.TenantID(), ID: in.SnapshotID})
		if e != nil {
			return e
		}
		if !finiteTimeBefore(in.ExpiresAt.Add(-time.Microsecond), until) {
			return ErrLeaseBoundary
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID})
		if e != nil {
			return e
		}
		if fence.RenewalClosed || in.FencingFloor < fence.FencingGeneration {
			return ErrShareConflict
		}
		rate, connections := int64(0), int64(0)
		generations := make(map[string]int64, 2)
		for _, kind := range []string{"rate", "connections"} {
			c, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind})
			if e != nil {
				return e
			}
			if c.BootID != in.BootID || c.PendingGeneration != 0 || c.ActiveGeneration < 1 {
				return ErrShareUnknown
			}
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind, Generation: c.ActiveGeneration})
			if e != nil {
				return e
			}
			if g.SignerID != in.HubID || in.FencingFloor < g.FencingGeneration || in.ExpiresAt.After(g.OwnershipUntil.Time) || in.ExpiresAt.After(g.PolicyUntil.Time) || in.ExpiresAt.After(g.AutonomyUntil.Time) {
				return ErrLeaseBoundary
			}
			authority, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: kind, Generation: g.Generation, SignerID: in.HubID})
			if e != nil {
				return e
			}
			barrier, e := q.ShareClosureBarrier(ctx, generated.ShareClosureBarrierParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID, Kind: kind, Generation: g.Generation, SignerID: in.HubID})
			if e != nil {
				return e
			}
			if barrier {
				return ErrShareConflict
			}
			issuer, e := effectiveShareIssuerTx(ctx, q, g, authority)
			if e != nil {
				return e
			}
			if issuer.Epoch != in.SignerEpoch || (issuer.Recovered && issuer.Boot != in.HubBootID) || (!issuer.Recovered && authority.ClosedAt.Valid) {
				return ErrShareConflict
			}
			if kind == "rate" {
				rate = min(g.Amount, limits.RateLimit)
			} else {
				connections = min(g.Amount, limits.ConnectionLimit)
			}
			generations[kind] = g.Generation
		}
		if ruleRate > 0 {
			rate = min(rate, ruleRate)
		}
		if ruleConnections > 0 {
			connections = min(connections, ruleConnections)
		}
		if rate < 1 || connections < 1 {
			return ErrShareCapacity
		}
		// Each allocation has a distinct delegated credit source. Its immutable PG
		// reservation links it to the original traffic source; multiple rules cannot
		// invalidate each other's SQLite source roots by sharing a root head.
		source, e := q.MeteringSource(ctx, generated.MeteringSourceParams{TenantID: scope.TenantID(), ID: in.SourceID, AtTime: timestamp(hi)})
		if e != nil {
			return e
		}
		if in.ExpiresAt.After(source.EffectiveUntil.Time) {
			return ErrLeaseBoundary
		}
		r, e := reserveUsageTx(ctx, q, scope, ReserveInput{SourceID: in.SourceID, SnapshotID: in.SnapshotID, NodeID: in.NodeID, BootID: in.BootID, Nonce: "hub/" + in.Nonce, RawBytes: in.RawBytes, Earliest: lo, Latest: hi, Deadline: in.ExpiresAt})
		if e != nil {
			return e
		}
		out = hub.Allocation{AllocationID: uuid.NewString(), DelegationID: uuid.NewString(), HubID: in.HubID, Region: in.Region, NodeID: in.NodeID, BootID: in.BootID, SourceID: r.ID, RuntimeSourceID: uuid.NewString(), ShareSourceID: uuid.NewString(), SnapshotID: b.ID, RuleID: b.RuleID, RuleRevision: uint64(b.RuleRevision), PathGeneration: uint64(in.PathGeneration), FencingFloor: uint64(in.FencingFloor), SignerEpoch: uint64(in.SignerEpoch), RawBytes: uint64(in.RawBytes), RateBPS: uint64(rate), Connections: uint64(connections), ValidFrom: hi, ExpiresAt: in.ExpiresAt.UTC(), HardDeadline: in.ExpiresAt.UTC(), BackendHeartbeat: lo, AutonomyDeadline: signer.AutonomyUntil.Time.UTC(), ShareReferences: []store.ShareReference{}, PolicyTransitions: []store.PolicyTransition{}}
		for _, source := range []string{out.SourceID, out.RuntimeSourceID, out.ShareSourceID} {
			// Every root belongs to a fresh source UUID and starts its own lineage.
			// Its generation cannot stand for either independent Node share kind.
			out.ShareReferences = append(out.ShareReferences, store.ShareReference{ID: uuid.NewString(), SourceID: source, IssuerID: out.HubID, NodeID: out.NodeID, SignerEpoch: out.SignerEpoch, Generation: 1, Revision: 1, RuleID: out.RuleID, RuleRevision: out.RuleRevision, PathGeneration: out.PathGeneration, RateBPS: out.RateBPS, Connections: out.Connections, NotBefore: out.ValidFrom, ExpiresAt: out.ExpiresAt, HardDeadline: out.HardDeadline})
		}
		if e = out.Validate(); e != nil {
			return e
		}
		payload, e := json.Marshal(out)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(payload)
		op, e := newDomainTask(ctx, q, scope, in.NodeID, "hub.allocation", "resource_change", in.SignerEpoch)
		if e != nil {
			return e
		}
		if e = q.AppendHubAllocation(ctx, generated.AppendHubAllocationParams{TenantID: scope.TenantID(), AllocationID: out.AllocationID, DelegationID: out.DelegationID, ReservationID: r.ID, HubID: in.HubID, HubBootID: in.HubBootID, NodeID: in.NodeID, BootID: in.BootID, RequestKey: in.Nonce, RequestDigest: digest[:], Payload: payload, PayloadDigest: sum[:], OperationID: op.ID, CreatedBy: scope.ActorID()}); e != nil {
			return e
		}
		for _, kind := range []string{"rate", "connections"} {
			if e = q.AppendHubAllocationShareBinding(ctx, generated.AppendHubAllocationShareBindingParams{TenantID: scope.TenantID(), AllocationID: out.AllocationID, NodeID: in.NodeID, Kind: kind, Generation: generations[kind]}); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		return hub.Allocation{}, e
	}
	return out, nil
}

// RecordHubIssue consumes the actual FULL-committed issuer journal. Child credit
// IDs partition a single funded parent; no child reserves money or rounds it
// again. A missing receipt leaves the entire parent hold in place.
func (s ShareService) RecordHubIssue(ctx context.Context, hubID, hubBootID string, sequence int64, payload []byte, request string) error {
	if sequence < 1 || len(payload) == 0 || len(payload) > 1<<20 {
		return ErrShareConflict
	}
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID); e != nil {
		return e
	}
	var head struct {
		NodeID         string `json:"node_id"`
		BootID         string `json:"boot_id"`
		PathGeneration uint64 `json:"path_generation"`
	}
	if json.Unmarshal(payload, &head) != nil {
		return ErrShareConflict
	}
	parsed, e := agent.InspectAuthorizationUpdate(payload, head.NodeID, head.BootID, head.PathGeneration)
	if e != nil {
		return e
	}
	if parsed.SchemaVersion != "1.2.0" || len(parsed.Records) != 3 {
		return ErrShareConflict
	}
	delegation := parsed.Records[0].DelegationID
	record, e := generated.New(s.Pool).HubAllocationByDelegation(ctx, delegation)
	if e != nil {
		return e
	}
	if record.HubID != hubID {
		return ErrScope
	}
	scope, e := NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/hub-issue/"+hubID)).String(), record.TenantID, record.TenantID)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(payload)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.record_issue", ResourceID: parsed.UpdateID, RequestID: request, Reason: "authenticated immutable Hub child issuance; original source-funded parent remains the accounting unit"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hubID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetHubIssue(ctx, generated.GetHubIssueParams{TenantID: scope.TenantID(), IssueID: parsed.UpdateID})
		if e == nil {
			if old.HubID != hubID || old.HubBootID != hubBootID {
				return ErrScope
			}
			if old.LedgerSequence != sequence || !bytes.Equal(old.Payload, payload) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = currentHubEpochTx(ctx, q, hubID, hubBootID, parsed.SignerEpoch); e != nil {
			return e
		}
		a, handoffs, e := effectiveHubAllocationTx(ctx, q, record, parsed.SignerEpoch, hubBootID, parsed.IssuedAt)
		if e != nil {
			return e
		}
		if len(parsed.Handoffs) != len(handoffs) || len(handoffs) > 0 && !jsonEqual(parsed.Handoffs, handoffs) {
			return ErrShareConflict
		}
		if parsed.NodeID != a.NodeID || parsed.BootID != a.BootID || parsed.SignerEpoch != a.SignerEpoch || parsed.PathGeneration != a.PathGeneration || parsed.FencingFloor != a.FencingFloor || parsed.IssuedAt.Before(a.ValidFrom) || !parsed.IssuedAt.Before(a.ExpiresAt) || parsed.BackendHeartbeat.Before(a.BackendHeartbeat) || !jsonEqual(parsed.ShareReferences, a.ShareReferences) {
			return ErrShareConflict
		}
		// Envelope liveness may reflect a newer backend heartbeat. Each child still
		// has to fit this allocation's original immutable hard deadline below.
		var credit agent.IssuedAuthorizationRecord
		seen := map[string]bool{}
		sources := map[string]string{"credit_grant": a.SourceID, "runtime_lease": a.RuntimeSourceID, "share_lease": a.ShareSourceID}
		for _, r := range parsed.Records {
			if seen[r.Kind] || r.ParentAuthorizationID != a.DelegationID || r.DelegationID != a.DelegationID || r.RuleID != a.RuleID || r.RuleRevision != a.RuleRevision || r.PathGeneration != a.PathGeneration || r.SourceID != sources[r.Kind] || r.ExpiresAt.After(a.ExpiresAt) || r.HardDeadline.After(a.HardDeadline) {
				return ErrShareConflict
			}
			ref := ""
			for _, v := range a.ShareReferences {
				if v.SourceID == r.SourceID {
					ref = v.ID
				}
			}
			if ref == "" || r.ShareReferenceID != ref {
				return ErrShareConflict
			}
			seen[r.Kind] = true
			if r.Kind == "credit_grant" {
				credit = r
				if r.BillingSnapshotID != a.SnapshotID || r.AmountBytes == nil || *r.AmountBytes > a.RawBytes {
					return ErrShareCapacity
				}
			}
			if r.Kind == "share_lease" && (r.RateBPS == nil || r.Connections == nil || *r.RateBPS > a.RateBPS || *r.Connections > a.Connections) {
				return ErrShareCapacity
			}
		}
		if credit.AmountBytes == nil || len(seen) != 3 {
			return ErrShareConflict
		}
		if _, e = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID}); e == nil {
			return ErrShareConflict
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		if e != nil {
			return e
		}
		if uint64(issued) > a.RawBytes || *credit.AmountBytes > a.RawBytes-uint64(issued) {
			return ErrShareCapacity
		}
		if e = q.AppendHubIssue(ctx, generated.AppendHubIssueParams{TenantID: scope.TenantID(), IssueID: parsed.UpdateID, AllocationID: a.AllocationID, HubID: hubID, HubBootID: hubBootID, LedgerSequence: sequence, Payload: payload, PayloadDigest: digest[:]}); e != nil {
			return e
		}
		if e = q.AppendHubCreditGrant(ctx, generated.AppendHubCreditGrantParams{TenantID: scope.TenantID(), GrantID: credit.AuthorizationID, AllocationID: a.AllocationID, IssueID: parsed.UpdateID, RawGranted: int64(*credit.AmountBytes)}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, credit.AuthorizationID, "hub.child_issued", "resource_change", sequence)
	})
}
