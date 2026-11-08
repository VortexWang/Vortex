package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"

	"github.com/jackc/pgx/v5"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
	"vortex/hub"
)

func sameStoreCommand(ctx context.Context, q *generated.Queries, id, hubID, boot string) (generated.HubSameStoreRecovery, hub.SameStoreRecoveryCommand, error) {
	r, e := q.HubSameStoreRecoveryByID(ctx, id)
	if e != nil {
		return r, hub.SameStoreRecoveryCommand{}, e
	}
	hash, proofHash := sha256.Sum256(r.Payload), sha256.Sum256(r.Proof)
	c, e := hub.DecodeSameStoreRecovery(r.Payload)
	if e != nil || !bytes.Equal(hash[:], r.PayloadDigest) || !bytes.Equal(proofHash[:], r.ProofDigest) || c.ID != id || c.HubID != hubID || c.ToBootID != boot || r.HubID != hubID || r.ToBootID != boot || r.FromEpoch != int64(c.Handoff.FromEpoch) || r.ToEpoch != int64(c.Handoff.ToEpoch) {
		return r, c, ErrShareConflict
	}
	return r, c, nil
}

func (s ShareService) pendingSameStore(ctx context.Context, q *generated.Queries, c hub.SameStoreRecoveryCommand) error {
	current, e := q.LatestHubIncarnation(ctx, c.HubID)
	if e != nil {
		return e
	}
	boundary, e := q.HubSignerBoundary(ctx, c.HubID)
	if e != nil {
		return e
	}
	if current.BootID != c.FromBootID || current.SignerEpoch != int64(c.Handoff.FromEpoch) || boundary.SignerEpoch != int64(c.Handoff.ToEpoch) {
		return ErrShareUnknown
	}
	lo, hi, e := s.bounds()
	if e != nil {
		return e
	}
	if lo.Before(c.IssuedAt) || !hi.Before(c.ExpiresAt) || !hi.Before(boundary.AutonomyUntil.Time) {
		return ErrLeaseBoundary
	}
	return nil
}

// RetainHubSameStoreRecord durably retains original prefix evidence. It is not
// a usage settlement, financial issue receipt, local journal ACK or admission.
// The original boot remains bound; separate complete verification is required.
func (s ShareService) RetainHubSameStoreRecord(ctx context.Context, hubID, boot, id string, r store.LedgerRecord, request string) error {
	if r.Sequence < 1 || r.Sequence == math.MaxInt64 || r.ID == "" || len(r.ID) > 256 || len(r.Payload) == 0 || len(r.Payload) > 1<<20 {
		return ErrShareConflict
	}
	switch r.Kind {
	case "issue", "runtime-lease", "delegation-seal", "known", "unknown", "handoff":
	default:
		return ErrShareConflict
	}
	hash := sha256.Sum256(r.Payload)
	if !bytes.Equal(hash[:], r.Digest) {
		return ErrShareConflict
	}
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID)
	if e != nil {
		return e
	}
	approval, _, e := sameStoreCommand(ctx, generated.New(s.Pool), id, hubID, boot)
	if e != nil {
		return e
	}
	scope, e := hubScope(hubID, approval.TenantID)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.retain_same_store_prefix", ResourceID: id, RequestID: request, Reason: "immutable recovery evidence and pending obligation only; no settlement, authority or online ACK"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hubID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		_, c, e := sameStoreCommand(ctx, q, id, hubID, boot)
		if e != nil {
			return e
		}
		old, e := q.GetHubSameStoreJournal(ctx, generated.GetHubSameStoreJournalParams{RecoveryID: id, Sequence: r.Sequence})
		if e == nil {
			if old.Kind != r.Kind || old.RecordID != r.ID || !bytes.Equal(old.Payload, r.Payload) || !bytes.Equal(old.PayloadDigest, r.Digest) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if _, e = q.GetHubSameStoreManifest(ctx, id); e == nil {
			return ErrShareConflict
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = s.pendingSameStore(ctx, q, c); e != nil {
			return e
		}
		op, e := newDomainTask(ctx, q, scope, id, "hub.same_store_prefix_retained", "resource_change", r.Sequence)
		if e != nil {
			return e
		}
		return q.AppendHubSameStoreJournal(ctx, generated.AppendHubSameStoreJournalParams{TenantID: scope.TenantID(), RecoveryID: id, Sequence: r.Sequence, Kind: r.Kind, RecordID: r.ID, Payload: r.Payload, PayloadDigest: r.Digest, PeerFingerprint: fingerprint, OperationID: op.ID})
	})
}

// VerifyHubSameStorePrefix independently reconstructs the complete retained
// prefix and original PG credit partitions. The resulting immutable witness
// retains all liabilities. It does not bind an incarnation or acknowledge
// online readiness; admission must recheck stale authority and financial state.
func (s ShareService) VerifyHubSameStorePrefix(ctx context.Context, hubID, boot, id string, raw []byte, request string) (generated.HubSameStoreManifest, error) {
	var out generated.HubSameStoreManifest
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID)
	if e != nil {
		return out, e
	}
	approval, c, e := sameStoreCommand(ctx, generated.New(s.Pool), id, hubID, boot)
	if e != nil {
		return out, e
	}
	m, e := hub.DecodeSameStoreManifest(raw, c)
	if e != nil {
		return out, e
	}
	scope, e := hubScope(hubID, approval.TenantID)
	if e != nil {
		return out, e
	}
	hash := sha256.Sum256(raw)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.verify_same_store_prefix", ResourceID: id, RequestID: request, Reason: "complete retained S/R prefix and independent PG issue inventory only; original unknown debt and admission remain held"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hubID); e != nil {
			return e
		}
		var proof hubSameStoreRecoveryProof
		if json.Unmarshal(approval.Proof, &proof) != nil {
			return ErrShareConflict
		}
		tenants := map[string]bool{scope.TenantID(): true}
		for _, a := range proof.Authorities {
			tenants[a.TenantID] = true
		}
		ordered := make([]string, 0, len(tenants))
		for tenant := range tenants {
			ordered = append(ordered, tenant)
		}
		sort.Strings(ordered)
		for _, tenant := range ordered {
			if _, e := q.LockTenant(ctx, tenant); e != nil {
				return e
			}
		}

		prior, e := q.GetHubSameStoreManifest(ctx, id)
		if e == nil {
			h := sha256.Sum256(prior.Payload)
			if !bytes.Equal(prior.Payload, raw) || !bytes.Equal(h[:], prior.PayloadDigest) {
				return ErrShareConflict
			}
			out = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = s.recheckSameStorePrefixTx(ctx, q, proof, c, m, raw); e != nil {
			return e
		}
		op, e := newDomainTask(ctx, q, scope, id, "hub.same_store_prefix_verified", "resource_change", int64(c.Handoff.ToEpoch))
		if e != nil {
			return e
		}
		if e = q.AppendHubSameStoreManifest(ctx, generated.AppendHubSameStoreManifestParams{TenantID: scope.TenantID(), RecoveryID: id, SealedSequence: m.Result.SealedSequence, Payload: raw, PayloadDigest: hash[:], PeerFingerprint: fingerprint, OperationID: op.ID}); e != nil {
			return e
		}
		out, e = q.GetHubSameStoreManifest(ctx, id)
		return e
	})
	if e != nil {
		return generated.HubSameStoreManifest{}, e
	}
	return out, nil
}

func verifySameStoreFinancialIssue(ctx context.Context, q *generated.Queries, hubID string, r generated.HubSameStoreJournal) error {
	var head struct {
		NodeID string `json:"node_id"`
		BootID string `json:"boot_id"`
		Path   uint64 `json:"path_generation"`
	}
	if json.Unmarshal(r.Payload, &head) != nil {
		return ErrShareConflict
	}
	p, e := agent.InspectAuthorizationUpdate(r.Payload, head.NodeID, head.BootID, head.Path)
	if e != nil {
		return e
	}
	if p.UpdateID != r.RecordID || len(p.Records) != 3 {
		return ErrShareConflict
	}
	a, e := q.HubAllocationByDelegation(ctx, p.Records[0].DelegationID)
	if e != nil {
		return e
	}
	if a.HubID != hubID {
		return ErrScope
	}
	prior, e := q.GetHubIssue(ctx, generated.GetHubIssueParams{TenantID: a.TenantID, IssueID: r.RecordID})
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrShareUnknown
		}
		return e
	}
	if prior.AllocationID != a.AllocationID || prior.HubID != hubID || prior.HubBootID != a.HubBootID || prior.LedgerSequence != r.Sequence || !bytes.Equal(prior.Payload, r.Payload) || !bytes.Equal(prior.PayloadDigest, r.PayloadDigest) {
		return ErrShareConflict
	}
	return nil
}

// recheckSameStorePrefixTx intentionally reruns the complete verifier at admission.
// A historical immutable witness is not evidence of unchanged current state.
func (s ShareService) recheckSameStorePrefixTx(ctx context.Context, q *generated.Queries, proof hubSameStoreRecoveryProof, c hub.SameStoreRecoveryCommand, m hub.SameStoreRecoveryManifest, raw []byte) error {
	hubID, id := c.HubID, c.ID
	if e := s.pendingSameStore(ctx, q, c); e != nil {
		return e
	}
	authorities, e := q.HubRecoveryAuthorities(ctx, generated.HubRecoveryAuthoritiesParams{SignerID: hubID, ToEpoch: int64(c.Handoff.ToEpoch)})
	if e != nil {
		return e
	}
	if !jsonEqual(authorities, proof.Authorities) {
		return ErrShareUnknown
	}
	count := int64(0)
	prefixHash := sha256.New()
	enc := json.NewEncoder(prefixHash)
	for {
		page, e := q.PageHubSameStoreJournal(ctx, generated.PageHubSameStoreJournalParams{RecoveryID: id, AfterSequence: count})
		if e != nil {
			return e
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if r.Sequence != count+1 || r.Sequence > m.Result.SealedSequence+1 {
				return ErrShareUnknown
			}
			h := sha256.Sum256(r.Payload)
			if !bytes.Equal(h[:], r.PayloadDigest) {
				return ErrShareConflict
			}
			if r.Sequence == m.Result.SealedSequence+1 {
				if r.Kind != "handoff" || r.RecordID != id || !bytes.Equal(r.Payload, raw) {
					return ErrShareConflict
				}
			} else {
				item := struct {
					Sequence         int64
					Kind, ID, SHA256 string
				}{r.Sequence, r.Kind, r.RecordID, hex.EncodeToString(h[:])}
				if e = enc.Encode(item); e != nil {
					return e
				}
				if r.Kind == "issue" {
					if e = verifySameStoreFinancialIssue(ctx, q, hubID, r); e != nil {
						return e
					}
				}
			}
			count++
		}
	}
	if count != m.Result.SealedSequence+1 || hex.EncodeToString(prefixHash.Sum(nil)) != m.Result.FundedInventory.PrefixSHA256 {
		return ErrShareUnknown
	}
	missing, e := q.MissingHubSameStoreIssues(ctx, generated.MissingHubSameStoreIssuesParams{HubID: hubID, HubBootID: c.FromBootID, RecoveryID: id, SealedSequence: m.Result.SealedSequence})
	if e != nil {
		return e
	}
	if missing {
		return ErrShareUnknown
	}
	for i, link := range c.Allocations {
		a, e := q.GetHubAllocationGlobal(ctx, link.AllocationID)
		if e != nil {
			return e
		}
		if _, e = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: a.TenantID, AllocationID: a.AllocationID}); e == nil {
			return ErrShareUnknown
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		allocation, e := hub.DecodeAllocation(a.Payload)
		if e != nil {
			return e
		}
		h := sha256.Sum256(a.Payload)
		if hex.EncodeToString(h[:]) != link.PayloadSHA256 {
			return ErrShareConflict
		}
		rows, e := q.HubAllocationIssueInventory(ctx, generated.HubAllocationIssueInventoryParams{TenantID: a.TenantID, AllocationID: a.AllocationID})
		if e != nil {
			return e
		}
		items := make([]contracts.IssuedPartition, 0, len(rows))
		for _, r := range rows {
			items = append(items, contracts.IssuedPartition{ID: r.IssueID, Digest: hex.EncodeToString(r.PayloadDigest), Raw: r.RawGranted})
		}
		digest, issued, e := contracts.PartitionDigest(items)
		if e != nil {
			return e
		}
		p := m.Result.FundedInventory.Delegations[i]
		if p.Raw != int64(allocation.RawBytes) || p.Issued != issued || p.IssueCount != int64(len(items)) || p.IssueSHA256 != digest {
			return ErrShareUnknown
		}
	}
	return nil
}
