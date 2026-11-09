package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type ShareCandidateReference struct {
	RuleID            string `json:"rule_id"`
	RuleRevision      int64  `json:"rule_revision"`
	SnapshotID        string `json:"billing_snapshot_id"`
	ArtifactDigest    string `json:"artifact_sha256"`
	DeploymentID      string `json:"deployment_id"`
	SignerEpoch       int64  `json:"signer_epoch"`
	PathGeneration    int64  `json:"path_generation"`
	FencingGeneration int64  `json:"fencing_generation"`
}
type NodeAllocation struct {
	NodeID      string                    `json:"node_id"`
	BootID      string                    `json:"boot_id"`
	RateBPS     int64                     `json:"rate_bps"`
	Connections int64                     `json:"connections"`
	Queued      bool                      `json:"queued"`
	References  []ShareCandidateReference `json:"references"`
}
type AllocationWindow struct {
	WindowAt    time.Time        `json:"window_at"`
	OperationID string           `json:"operation_id"`
	Nodes       []NodeAllocation `json:"nodes"`
}

// DemandSample contains averages over exactly one completed UTC minute. This
// is authenticated scheduling input, never a credit or execution receipt.
type DemandSample struct {
	SampleID                string    `json:"sample_id"`
	NodeID                  string    `json:"node_id"`
	BootID                  string    `json:"boot_id"`
	WindowAt                time.Time `json:"window_at"`
	RateDemand              int64     `json:"rate_demand"`
	ConnectionDemand        int64     `json:"connection_demand"`
	RateExhaustionPPM       int32     `json:"rate_exhaustion_ppm"`
	ConnectionExhaustionPPM int32     `json:"connection_exhaustion_ppm"`
}

func (s ShareService) RecordDemand(ctx context.Context, scope TenantScope, in DemandSample, request string) error {
	if !validID(in.SampleID) || !validID(in.NodeID) || !validID(in.BootID) || in.RateDemand < 0 || in.ConnectionDemand < 0 || in.RateExhaustionPPM < 0 || in.RateExhaustionPPM > 1000000 || in.ConnectionExhaustionPPM < 0 || in.ConnectionExhaustionPPM > 1000000 {
		return ErrAllocationInput
	}
	if _, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, in.NodeID); e != nil {
		return e
	}
	_, now, e := s.bounds()
	if e != nil {
		return e
	}
	in.WindowAt = in.WindowAt.UTC()
	if !in.WindowAt.Equal(now.Truncate(time.Minute).Add(-time.Minute)) {
		return ErrAllocationInput
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.demand", ResourceID: in.NodeID, RequestID: request, Reason: "authenticated completed-minute scheduling sample; no ownership change"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetShareDemandSample(ctx, generated.GetShareDemandSampleParams{TenantID: scope.TenantID(), NodeID: in.NodeID, WindowAt: timestamp(in.WindowAt)})
		if e == nil {
			if !bytes.Equal(raw, old.Payload) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		node, e := q.GetNodeIncarnation(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != in.BootID {
			return ErrShareConflict
		}
		candidates, e := q.PreparedShareArtifacts(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		found := false
		for _, a := range candidates {
			found = found || a.NodeID == in.NodeID && a.BootID == in.BootID
		}
		if !found {
			return ErrScope
		}
		if e = q.AppendShareDemandSample(ctx, generated.AppendShareDemandSampleParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID, SampleID: in.SampleID, WindowAt: timestamp(in.WindowAt), Payload: raw, RateDemand: in.RateDemand, ConnectionDemand: in.ConnectionDemand, RateExhaustionPpm: in.RateExhaustionPPM, ConnectionExhaustionPpm: in.ConnectionExhaustionPPM}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, in.NodeID, "share.demand", "resource_change", in.WindowAt.Unix())
	})
}

// AllocateWindow commits exactly one target snapshot per trusted UTC minute.
// It uses current desired enabled revisions with successful durable Prepare;
// active_revision/actual_version and historical sessions are not candidates.
// Every rule reference is retained while node capacity is counted only once.
func (s ShareService) AllocateWindow(ctx context.Context, scope TenantScope, request string) (AllocationWindow, error) {
	var out AllocationWindow
	_, now, e := s.bounds()
	if e != nil {
		return out, e
	}
	window := now.Truncate(time.Minute)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.allocate", ResourceID: scope.TenantID(), RequestID: request, Reason: "persist 60-second prepared-candidate targets and FIFO queue; existing unknown ownership retained"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetShareAllocationWindow(ctx, generated.GetShareAllocationWindowParams{TenantID: scope.TenantID(), WindowAt: timestamp(window)})
		if e == nil {
			return json.Unmarshal(old.Payload, &out)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		candidates, e := q.PreparedShareArtifacts(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		nodes := map[string]*NodeAllocation{}
		var terms PackageTerms
		for _, a := range candidates {
			eligibility := PrepareShareInput{NodeID: a.NodeID, BootID: a.BootID, SnapshotID: a.BillingSnapshotID, PathGeneration: a.PathGeneration, PolicyUntil: now.Add(time.Microsecond)}
			limits, _, e := shareEligibility(ctx, q, scope, eligibility, now)
			if allocationIneligible(e) {
				continue
			}
			if e != nil {
				return e
			}
			// Verify digest, canonical artifact and immutable compiler binding before
			// counting a prepared target. A mutable member projection alone is no proof.
			if _, e = repository.CommittedArtifact(ctx, q, scope.TenantID(), repository.ArtifactBinding{RuleID: a.RuleID, NodeID: a.NodeID, Digest: a.Digest, Revision: a.RuleRevision, FencingGeneration: a.FencingGeneration}); e != nil {
				return e
			}
			pinned, e := q.GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: scope.TenantID(), RuleID: a.RuleID, RuleRevision: a.RuleRevision, InputDigest: a.InputDigest})
			if e != nil {
				return e
			}
			var previous compiler.CompileInput
			if e = json.Unmarshal(pinned.Canonical, &previous); e != nil {
				return e
			}
			current, e := lockedCompileInput(ctx, q, scope, a.RuleID, a.RuleRevision, previous.Billing, a.SignerEpoch, a.PathGeneration, a.FencingGeneration)
			if allocationIneligible(e) {
				continue
			}
			if e != nil {
				return e
			}
			current.ValidAt = previous.ValidAt
			compiled := compiler.Compile(ctx, current)
			if len(compiled.Diagnostics) != 0 || compiled.InputDigest != a.InputDigest {
				continue
			}
			terms = limits
			n := nodes[a.NodeID]
			if n == nil {
				n = &NodeAllocation{NodeID: a.NodeID, BootID: a.BootID, References: []ShareCandidateReference{}}
				nodes[a.NodeID] = n
			}
			if n.BootID != a.BootID {
				return ErrShareConflict
			}
			n.References = append(n.References, ShareCandidateReference{RuleID: a.RuleID, RuleRevision: a.RuleRevision, SnapshotID: a.BillingSnapshotID, ArtifactDigest: a.Digest, DeploymentID: a.DeploymentID, SignerEpoch: a.SignerEpoch, PathGeneration: a.PathGeneration, FencingGeneration: a.FencingGeneration})
		}
		samples, e := q.ShareDemandSamples(ctx, generated.ShareDemandSamplesParams{TenantID: scope.TenantID(), WindowAt: timestamp(window.Add(-time.Minute))})
		if e != nil {
			return e
		}
		demand := map[string]generated.ShareDemandSample{}
		for _, d := range samples {
			demand[d.NodeID] = d
		}
		queues, e := q.GetShareAllocationQueue(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		order := map[string]int64{}
		for _, v := range queues {
			key := v.NodeID + "/" + v.Kind
			if v.State != "ineligible" {
				order[key] = v.QueuedAt.Time.UnixMicro()
			}
		}
		ids := make([]string, 0, len(nodes))
		for id := range nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rate, connections := make([]AllocationDemand, 0, len(ids)), make([]AllocationDemand, 0, len(ids))
		for _, id := range ids {
			d := demand[id]
			if d.BootID != nodes[id].BootID {
				d = generated.ShareDemandSample{}
			}
			for _, kind := range []string{"rate", "connections"} {
				key := id + "/" + kind
				if order[key] == 0 {
					order[key] = now.UnixMicro()
				}
			}
			rate = append(rate, AllocationDemand{NodeID: id, Demand: uint64(d.RateDemand), ExhaustionPPM: uint32(d.RateExhaustionPpm), QueueOrder: order[id+"/rate"]})
			connections = append(connections, AllocationDemand{NodeID: id, Demand: uint64(d.ConnectionDemand), ExhaustionPPM: uint32(d.ConnectionExhaustionPpm), QueueOrder: order[id+"/connections"]})
		}
		rates, e := DistributeShares(terms.RateLimit, false, rate)
		if e != nil {
			return e
		}
		slots, e := DistributeShares(terms.ConnectionLimit, true, connections)
		if e != nil {
			return e
		}
		out = AllocationWindow{WindowAt: window, Nodes: []NodeAllocation{}}
		for i, id := range ids {
			n := nodes[id]
			n.RateBPS = rates[i].Amount
			n.Connections = slots[i].Amount
			n.Queued = n.RateBPS == 0 || n.Connections == 0
			out.Nodes = append(out.Nodes, *n)
		}
		op, e := newDomainTask(ctx, q, scope, scope.TenantID(), "share.allocation_window", "share_reconcile", window.Unix())
		if e != nil {
			return e
		}
		out.OperationID = op.ID
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		if e = q.AppendShareAllocationWindow(ctx, generated.AppendShareAllocationWindowParams{TenantID: scope.TenantID(), WindowAt: timestamp(window), Payload: raw, PayloadDigest: sum[:], CreatedBy: scope.ActorID(), OperationID: op.ID}); e != nil {
			return e
		}
		if e = q.ResetShareAllocationQueues(ctx, generated.ResetShareAllocationQueuesParams{TenantID: scope.TenantID(), WindowAt: timestamp(window)}); e != nil {
			return e
		}
		for _, n := range out.Nodes {
			for _, kind := range []string{"rate", "connections"} {
				target := n.RateBPS
				if kind == "connections" {
					target = n.Connections
				}
				state := "target"
				if target == 0 {
					state = "queued"
				}
				if e = q.SetShareAllocationQueue(ctx, generated.SetShareAllocationQueueParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind, QueuedAt: timestamp(time.UnixMicro(order[n.NodeID+"/"+kind])), WindowAt: timestamp(window), Target: target, State: state}); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return AllocationWindow{}, e
	}
	return out, nil
}

func allocationIneligible(e error) bool {
	if e == nil {
		return false
	}
	var diagnostic compiler.Diagnostic
	return errors.Is(e, ErrSelfHostedExempt) || errors.Is(e, ErrScope) || errors.Is(e, ErrShareConflict) || errors.Is(e, ErrLeaseBoundary) || errors.Is(e, ErrConflict) || errors.Is(e, pgx.ErrNoRows) || errors.As(e, &diagnostic)
}
