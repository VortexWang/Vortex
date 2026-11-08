package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// PrepareNodeRecoverySources derives next roots only from an explicit approved
// handoff with a verified finite manifest and authenticated Node FULL fence.
// It never rewrites the original provision/allocation or admits the Hub.
func (s ShareService) PrepareNodeRecoverySources(ctx context.Context, scope TenantScope, recovery, allocation, request string) (generated.NodeRecoverySourceProvision, error) {
	var out generated.NodeRecoverySourceProvision
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	q := generated.New(s.Pool)
	approval, e := q.HubSameStoreRecoveryByID(ctx, recovery)
	if e != nil {
		return out, e
	}
	original, e := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: allocation})
	if e != nil {
		return out, e
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "node.prepare_recovery_sources", ResourceID: allocation, RequestID: request, Reason: "exact next roots after authenticated FULL inventory fence; no runtime rights or Hub admission"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+approval.HubID); e != nil {
			return e
		}
		if e := q.LockNodeAuthorityWrite(ctx, original.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		prior, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: scope.TenantID(), RecoveryID: recovery, AllocationID: allocation})
		if e == nil {
			if _, e = validateNodeRecoverySources(prior); e != nil {
				return e
			}
			out = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		_, c, e := sameStoreCommand(ctx, q, recovery, approval.HubID, approval.ToBootID)
		if e != nil {
			return e
		}
		if e = s.pendingSameStore(ctx, q, c); e != nil {
			return e
		}
		var proof hubSameStoreRecoveryProof
		if json.Unmarshal(approval.Proof, &proof) != nil {
			return ErrShareConflict
		}
		authorities, e := q.HubRecoveryAuthorities(ctx, generated.HubRecoveryAuthoritiesParams{SignerID: c.HubID, ToEpoch: int64(c.Handoff.ToEpoch)})
		if e != nil {
			return e
		}
		if !jsonEqual(authorities, proof.Authorities) {
			return ErrShareUnknown
		}
		fence, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: scope.TenantID(), RecoveryID: recovery, AllocationID: allocation})
		if e != nil {
			return e
		}
		f, e := validateNodeRecoveryCreditFence(fence)
		if e != nil {
			return e
		}
		receipt, e := q.GetNodeRecoveryCreditFenceReceipt(ctx, generated.GetNodeRecoveryCreditFenceReceiptParams{TenantID: scope.TenantID(), FenceID: fence.ID})
		if e != nil {
			return e
		}
		var r store.RecoveryCreditFenceReceipt
		rh := sha256.Sum256(receipt.Payload)
		if json.Unmarshal(receipt.Payload, &r) != nil || !bytes.Equal(rh[:], receipt.PayloadDigest) || r.Version != 1 || r.ID != f.ID || r.RecoveryID != recovery || r.NodeID != f.NodeID || r.BootID != f.BootID || r.BackendID != c.BackendID || r.Digest != hex.EncodeToString(fence.PayloadDigest) || len(receipt.PeerFingerprint) != 32 {
			return ErrShareConflict
		}
		canonical, _ := json.Marshal(r)
		if !bytes.Equal(canonical, receipt.Payload) || r.Installed == nil {
			return ErrShareConflict
		}
		allowed := map[string]uint64{}
		for _, g := range f.Credits {
			allowed[g.GrantID] = g.Amount
		}
		last := ""
		for _, g := range r.Installed {
			if g.GrantID <= last || allowed[g.GrantID] != g.Amount || g.Amount == 0 {
				return ErrShareConflict
			}
			last = g.GrantID
		}
		witness, e := q.GetHubSameStoreManifest(ctx, recovery)
		if e != nil {
			return e
		}
		mh := sha256.Sum256(witness.Payload)
		if !bytes.Equal(mh[:], witness.PayloadDigest) || hex.EncodeToString(mh[:]) != f.ManifestSHA256 {
			return ErrShareConflict
		}
		m, e := hub.DecodeSameStoreManifest(witness.Payload, c)
		if e != nil {
			return e
		}
		index := -1
		for i, a := range c.Allocations {
			if a.AllocationID == allocation {
				index = i
				break
			}
		}
		if index < 0 {
			return ErrShareConflict
		}
		a, e := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if e != nil {
			return e
		}
		h := sha256.Sum256(a.Payload)
		if !bytes.Equal(h[:], a.PayloadDigest) || hex.EncodeToString(h[:]) != c.Allocations[index].PayloadSHA256 || a.HubID != c.HubID || a.HubBootID != c.FromBootID || a.NodeID != f.NodeID || a.BootID != f.BootID || a.DelegationID != f.ParentID {
			return ErrShareConflict
		}
		decoded, e := hub.DecodeAllocation(a.Payload)
		if e != nil {
			return e
		}
		if _, e = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: a.TenantID, AllocationID: allocation}); e == nil {
			return ErrShareUnknown
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		node, e := q.LockNodeRecoveryIncarnation(ctx, a.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != a.BootID {
			return ErrShareUnknown
		}
		grants, e := q.GetHubCreditGrantsByAllocation(ctx, generated.GetHubCreditGrantsByAllocationParams{TenantID: a.TenantID, AllocationID: allocation})
		if e != nil {
			return e
		}
		expected := make([]store.RecoveryCreditEntry, 0, len(grants))
		for _, g := range grants {
			if g.RawGranted <= 0 {
				return ErrShareConflict
			}
			expected = append(expected, store.RecoveryCreditEntry{GrantID: g.GrantID, Amount: uint64(g.RawGranted)})
		}
		if !jsonEqual(expected, f.Credits) || f.OriginalRaw != decoded.RawBytes || f.Issued != uint64(m.Result.FundedInventory.Delegations[index].Issued) || f.Remaining != f.OriginalRaw-f.Issued {
			return ErrShareUnknown
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		if !hi.Before(f.ExpiresAt) || !hi.Before(decoded.ExpiresAt) || !hi.Before(decoded.HardDeadline) || !hi.Before(decoded.AutonomyDeadline) {
			return ErrLeaseBoundary
		}
		successor := c.Handoff.FundedSuccessors[index]
		if successor.DelegationID != a.DelegationID || len(successor.ShareReferences) != len(decoded.ShareReferences) {
			return ErrShareConflict
		}
		for i, old := range decoded.ShareReferences {
			next := old
			next.Revision++
			next.SignerEpoch = c.Handoff.ToEpoch
			if !jsonEqual(next, successor.ShareReferences[i]) {
				return ErrShareConflict
			}
		}
		p := store.SourceProvision{Version: 1, ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("node-recovery-sources/"+recovery+"/"+allocation)).String(), BackendID: c.BackendID, NodeID: a.NodeID, BootID: a.BootID, IssuedAt: lo, Roots: successor.ShareReferences}
		raw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = store.DecodeSourceProvision(raw); e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		op, e := newDomainTask(ctx, q, scope, a.NodeID, "node.provision_recovery_sources", "resource_change", int64(c.Handoff.ToEpoch))
		if e != nil {
			return e
		}
		if e = q.AppendNodeRecoverySources(ctx, generated.AppendNodeRecoverySourcesParams{TenantID: a.TenantID, ID: p.ID, RecoveryID: recovery, AllocationID: allocation, FenceID: f.ID, NodeID: a.NodeID, BootID: a.BootID, BackendID: c.BackendID, OperationID: op.ID, CreatedBy: scope.ActorID(), Payload: raw, PayloadDigest: sum[:]}); e != nil {
			return e
		}
		out, e = q.GetNodeRecoverySources(ctx, generated.GetNodeRecoverySourcesParams{TenantID: a.TenantID, ID: p.ID})
		return e
	})
	if e != nil {
		return generated.NodeRecoverySourceProvision{}, e
	}
	return out, nil
}

func validateNodeRecoverySources(p generated.NodeRecoverySourceProvision) (store.SourceProvision, error) {
	c, e := store.DecodeSourceProvision(p.Payload)
	h := sha256.Sum256(p.Payload)
	if e != nil || !bytes.Equal(h[:], p.PayloadDigest) || c.ID != p.ID || c.BackendID != p.BackendID || c.NodeID != p.NodeID || c.BootID != p.BootID {
		return c, ErrShareConflict
	}
	return c, nil
}

// RecordNodeRecoverySources accepts only the direct Node FULL source receipt.
// This is immutable installation evidence, never an admission/ledger ACK.
func (s ShareService) RecordNodeRecoverySources(ctx context.Context, scope TenantScope, r store.SourceProvisionReceipt, request string) error {
	fingerprint, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.recovery_sources_installed", ResourceID: r.ID, RequestID: request, Reason: "authenticated FULL next roots only; no Hub admission, runtime right, settlement or capacity release"}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityWrite(ctx, r.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		p, e := q.GetNodeRecoverySources(ctx, generated.GetNodeRecoverySourcesParams{TenantID: scope.TenantID(), ID: r.ID})
		if e != nil {
			return e
		}
		if _, e = validateNodeRecoverySources(p); e != nil {
			return e
		}
		if r.Version != 1 || r.NodeID != p.NodeID || r.BootID != p.BootID || r.BackendID != p.BackendID || r.Digest != hex.EncodeToString(p.PayloadDigest) {
			return ErrStaleReceipt
		}
		old, e := q.GetNodeRecoverySourceReceipt(ctx, generated.GetNodeRecoverySourceReceiptParams{TenantID: scope.TenantID(), ProvisionID: r.ID})
		if e == nil {
			h := sha256.Sum256(old.Payload)
			if !bytes.Equal(old.Payload, raw) || !bytes.Equal(h[:], old.PayloadDigest) || !bytes.Equal(old.PeerFingerprint, fingerprint) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		node, e := q.LockNodeRecoveryIncarnation(ctx, p.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != p.BootID {
			return ErrShareUnknown
		}
		if e = q.AppendNodeRecoverySourceReceipt(ctx, generated.AppendNodeRecoverySourceReceiptParams{TenantID: scope.TenantID(), ProvisionID: r.ID, Payload: raw, PayloadDigest: sum[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: p.OperationID})
	})
}
