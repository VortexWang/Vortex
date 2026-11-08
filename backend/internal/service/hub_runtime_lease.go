package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/agent"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// RecordHubRuntimeLease accepts authenticated lease-only journal entries. It
// does not create a credit mapping, reserve funds, release a parent or alter the
// immutable delegation. Historical replay stays historical even after expiry.
func (s ShareService) RecordHubRuntimeLease(ctx context.Context, hubID, boot string, sequence int64, payload []byte, request string) error {
	if !validID(hubID) || !validID(boot) || sequence < 1 || len(payload) == 0 || len(payload) > 1<<20 {
		return ErrShareConflict
	}
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID)
	if e != nil {
		return e
	}
	var head struct {
		Node string `json:"node_id"`
		Boot string `json:"boot_id"`
		Path uint64 `json:"path_generation"`
	}
	if json.Unmarshal(payload, &head) != nil {
		return ErrShareConflict
	}
	p, e := agent.InspectAuthorizationUpdate(payload, head.Node, head.Boot, head.Path)
	if e != nil {
		return e
	}
	if p.SchemaVersion != "1.2.0" || (len(p.Records) != 2 && len(p.Records) != 0) || len(p.ShareReferences) != 3 {
		return ErrShareConflict
	}
	var record generated.HubFundedAllocation
	if len(p.Records) == 0 {
		record, e = generated.New(s.Pool).HubAllocationByReservation(ctx, p.ShareReferences[0].SourceID)
	} else {
		record, e = generated.New(s.Pool).HubAllocationByDelegation(ctx, p.Records[0].DelegationID)
	}
	if e != nil {
		return e
	}
	if record.HubID != hubID || record.HubBootID != boot {
		return ErrScope
	}
	a, e := hub.DecodeAllocation(record.Payload)
	if e != nil {
		return e
	}
	if p.NodeID != a.NodeID || p.BootID != a.BootID || p.SignerEpoch != a.SignerEpoch || p.PathGeneration != a.PathGeneration || p.FencingFloor != a.FencingFloor || p.IssuedAt.Before(a.ValidFrom) || !p.IssuedAt.Before(a.ExpiresAt) || p.BackendHeartbeat.Before(a.BackendHeartbeat) || !jsonEqual(p.ShareReferences, a.ShareReferences) {
		return ErrShareConflict
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(payload, &envelope) != nil {
		return ErrShareConflict
	}
	for _, key := range []string{"renewal_request_id", "renewal_request_sha256", "epoch_handoffs"} {
		if _, ok := envelope[key]; ok {
			return ErrShareConflict
		}
	}
	for _, key := range []string{"policy_transitions", "revocations"} {
		var values []json.RawMessage
		if json.Unmarshal(envelope[key], &values) != nil || len(values) != 0 {
			return ErrShareConflict
		}
	}
	sources := map[string]string{"runtime_lease": a.RuntimeSourceID, "share_lease": a.ShareSourceID}
	seen := map[string]bool{}
	for _, r := range p.Records {
		if sources[r.Kind] == "" || seen[r.Kind] || r.SourceID != sources[r.Kind] || r.ParentAuthorizationID != a.DelegationID || r.DelegationID != a.DelegationID || r.RuleID != a.RuleID || r.RuleRevision != a.RuleRevision || r.PathGeneration != a.PathGeneration || !r.IssuedAt.Equal(p.IssuedAt) || r.NotBefore.Before(r.IssuedAt) || r.ExpiresAt.Sub(r.IssuedAt) > 300*time.Second || r.ExpiresAt.After(a.ExpiresAt) || r.HardDeadline.After(a.HardDeadline) || r.AmountBytes != nil || r.BillingSnapshotID != "" {
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
		if r.Kind == "share_lease" && (r.RateBPS == nil || r.Connections == nil || *r.RateBPS > a.RateBPS || *r.Connections > a.Connections) {
			return ErrShareCapacity
		}
		if r.Kind == "runtime_lease" && (r.RateBPS != nil || r.Connections != nil) {
			return ErrShareConflict
		}
		seen[r.Kind] = true
	}
	scope, e := hubScope(hubID, record.TenantID)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(payload)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.runtime_lease", ResourceID: p.UpdateID, RequestID: request, Reason: "authenticated immutable lease-only refresh; no credit issuance or financial mutation"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetHubRuntimeLease(ctx, generated.GetHubRuntimeLeaseParams{TenantID: scope.TenantID(), IssueID: p.UpdateID})
		if e == nil {
			if old.HubID != hubID || old.HubBootID != boot || old.LedgerSequence != sequence || !bytes.Equal(old.Payload, payload) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		if e != nil {
			return e
		}
		if issued < 1 {
			return ErrShareUnknown
		}
		previous, e := q.LatestHubRuntimeAuthorityTime(ctx, generated.LatestHubRuntimeAuthorityTimeParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		interval := time.Minute
		if len(p.Records) == 0 {
			interval = 10 * time.Second
			previous, e = q.LatestHubRuntimeHeartbeatTime(ctx, generated.LatestHubRuntimeHeartbeatTimeParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		}
		if e != nil {
			return e
		}
		if !previous.Valid || p.IssuedAt.Before(previous.Time.Add(interval)) {
			return ErrLeaseBoundary
		}
		if e = q.AppendHubRuntimeLease(ctx, generated.AppendHubRuntimeLeaseParams{TenantID: scope.TenantID(), IssueID: p.UpdateID, AllocationID: a.AllocationID, HubID: hubID, HubBootID: boot, LedgerSequence: sequence, Payload: payload, PayloadDigest: digest[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, p.UpdateID, "hub.runtime_lease", "resource_change", sequence)
	})
}
