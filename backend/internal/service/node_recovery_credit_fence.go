package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// PrepareNodeRecoveryCreditFence derives a finite inventory from an already
// verified immutable S/R manifest. It neither acknowledges that prefix nor
// releases its liabilities or activates the successor incarnation.
func (s ShareService) PrepareNodeRecoveryCreditFence(ctx context.Context, scope TenantScope, recovery, allocation, request string) (generated.NodeRecoveryCreditFence, error) {
	var out generated.NodeRecoveryCreditFence
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	q := generated.New(s.Pool)
	original, e := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: allocation})
	if e != nil {
		return out, e
	}
	approval, e := q.HubSameStoreRecoveryByID(ctx, recovery)
	if e != nil {
		return out, e
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "node.prepare_recovery_credit_fence", ResourceID: allocation, RequestID: request, Reason: "freeze installed original-source inventory; no activation, ACK or financial release"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+approval.HubID); e != nil {
			return e
		}
		if e := q.LockNodeAuthorityWrite(ctx, original.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		prior, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: scope.TenantID(), RecoveryID: recovery, AllocationID: allocation})
		if e == nil {
			if _, e = validateNodeRecoveryCreditFence(prior); e != nil {
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
		witness, e := q.GetHubSameStoreManifest(ctx, recovery)
		if e != nil {
			return e
		}
		mh := sha256.Sum256(witness.Payload)
		if !bytes.Equal(mh[:], witness.PayloadDigest) {
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
		if hex.EncodeToString(h[:]) != c.Allocations[index].PayloadSHA256 || !bytes.Equal(h[:], a.PayloadDigest) || a.HubID != c.HubID || a.HubBootID != c.FromBootID {
			return ErrShareConflict
		}
		decoded, e := hub.DecodeAllocation(a.Payload)
		if e != nil {
			return e
		}
		node, e := q.LockNodeRecoveryIncarnation(ctx, a.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != a.BootID {
			return ErrShareUnknown
		}
		if _, e = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: a.TenantID, AllocationID: a.AllocationID}); e == nil {
			return ErrShareUnknown
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		provision, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: a.TenantID, AllocationID: a.AllocationID})
		if e != nil {
			return e
		}
		source, e := store.DecodeSourceProvision(provision.Payload)
		ph := sha256.Sum256(provision.Payload)
		if e != nil || !bytes.Equal(ph[:], provision.PayloadDigest) || source.BackendID != c.BackendID || provision.BackendID != c.BackendID || source.NodeID != a.NodeID || source.BootID != a.BootID || !jsonEqual(source.Roots, decoded.ShareReferences) {
			return ErrShareConflict
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		p := store.RecoveryCreditFence{Version: 1, ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("node-recovery-credit-fence/"+recovery+"/"+allocation)).String(), RecoveryID: recovery, BackendID: c.BackendID, HubID: c.HubID, NodeID: a.NodeID, BootID: a.BootID, ParentID: a.DelegationID, SnapshotID: decoded.SnapshotID, ManifestSHA256: hex.EncodeToString(mh[:]), SealedSequence: m.Result.SealedSequence, IssuedAt: lo, ExpiresAt: hi.Add(4 * time.Minute), FromEpoch: c.Handoff.FromEpoch, ToEpoch: c.Handoff.ToEpoch, OriginalRaw: decoded.RawBytes, Credits: []store.RecoveryCreditEntry{}}
		for _, root := range decoded.ShareReferences {
			if root.SourceID == decoded.SourceID {
				p.Root = root
			}
		}
		if !hi.Before(decoded.ExpiresAt) || !hi.Before(decoded.HardDeadline) || !hi.Before(p.Root.ExpiresAt) || !hi.Before(p.Root.HardDeadline) {
			return ErrLeaseBoundary
		}
		for _, end := range []time.Time{c.ExpiresAt, decoded.ExpiresAt, decoded.HardDeadline, p.Root.ExpiresAt, p.Root.HardDeadline} {
			if end.Before(p.ExpiresAt) {
				p.ExpiresAt = end
			}
		}
		grants, e := q.GetHubCreditGrantsByAllocation(ctx, generated.GetHubCreditGrantsByAllocationParams{TenantID: a.TenantID, AllocationID: allocation})
		if e != nil {
			return e
		}
		for _, g := range grants {
			if g.RawGranted <= 0 || uint64(g.RawGranted) > p.OriginalRaw-p.Issued {
				return ErrShareConflict
			}
			p.Issued += uint64(g.RawGranted)
			p.Credits = append(p.Credits, store.RecoveryCreditEntry{GrantID: g.GrantID, Amount: uint64(g.RawGranted)})
		}
		partition := m.Result.FundedInventory.Delegations[index]
		if partition.Issued != int64(p.Issued) || partition.Raw != int64(p.OriginalRaw) {
			return ErrShareUnknown
		}
		p.Remaining = p.OriginalRaw - p.Issued
		raw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = store.DecodeRecoveryCreditFence(raw); e != nil {
			return e
		}
		hash := sha256.Sum256(raw)
		op, e := newDomainTask(ctx, q, scope, a.NodeID, "node.recovery_credit_fence", "resource_change", int64(p.ToEpoch))
		if e != nil {
			return e
		}
		if e = q.AppendNodeRecoveryCreditFence(ctx, generated.AppendNodeRecoveryCreditFenceParams{TenantID: a.TenantID, ID: p.ID, RecoveryID: recovery, AllocationID: allocation, NodeID: a.NodeID, BootID: a.BootID, BackendID: c.BackendID, OperationID: op.ID, CreatedBy: scope.ActorID(), Payload: raw, PayloadDigest: hash[:]}); e != nil {
			return e
		}
		out, e = q.GetNodeRecoveryCreditFence(ctx, generated.GetNodeRecoveryCreditFenceParams{TenantID: a.TenantID, ID: p.ID})
		return e
	})
	if e != nil {
		return generated.NodeRecoveryCreditFence{}, e
	}
	return out, nil
}

func validateNodeRecoveryCreditFence(p generated.NodeRecoveryCreditFence) (store.RecoveryCreditFence, error) {
	c, e := store.DecodeRecoveryCreditFence(p.Payload)
	h := sha256.Sum256(p.Payload)
	if e != nil || !bytes.Equal(h[:], p.PayloadDigest) || c.ID != p.ID || c.RecoveryID != p.RecoveryID || c.NodeID != p.NodeID || c.BootID != p.BootID || c.BackendID != p.BackendID {
		return c, ErrShareConflict
	}
	return c, nil
}

// RecordNodeRecoveryCreditFence records only authenticated FULL evidence that
// the Node found no hidden original-source child. Installed may be a subset of
// PG's list (a delivered Hub issue need not have reached that Node yet).
func (s ShareService) RecordNodeRecoveryCreditFence(ctx context.Context, scope TenantScope, raw []byte, request string) error {
	var r store.RecoveryCreditFenceReceipt
	if len(raw) == 0 || len(raw) > 1<<20 || json.Unmarshal(raw, &r) != nil || r.Installed == nil {
		return ErrStaleReceipt
	}
	canonical, e := json.Marshal(r)
	if e != nil || !bytes.Equal(canonical, raw) {
		return ErrStaleReceipt
	}
	fingerprint, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.recovery_credit_fenced", ResourceID: r.ID, RequestID: request, Reason: "authenticated FULL original-source fence; no admission, ACK, settlement or release"}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityWrite(ctx, r.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		p, e := q.GetNodeRecoveryCreditFence(ctx, generated.GetNodeRecoveryCreditFenceParams{TenantID: scope.TenantID(), ID: r.ID})
		if e != nil {
			return e
		}
		c, e := validateNodeRecoveryCreditFence(p)
		if e != nil {
			return e
		}
		if r.Version != 1 || r.RecoveryID != c.RecoveryID || r.BackendID != c.BackendID || r.NodeID != c.NodeID || r.BootID != c.BootID || r.Digest != hex.EncodeToString(p.PayloadDigest) {
			return ErrStaleReceipt
		}
		allowed := map[string]uint64{}
		for _, g := range c.Credits {
			allowed[g.GrantID] = g.Amount
		}
		previous := ""
		for _, g := range r.Installed {
			if g.GrantID <= previous || allowed[g.GrantID] != g.Amount || g.Amount == 0 {
				return ErrStaleReceipt
			}
			previous = g.GrantID
		}
		prior, e := q.GetNodeRecoveryCreditFenceReceipt(ctx, generated.GetNodeRecoveryCreditFenceReceiptParams{TenantID: scope.TenantID(), FenceID: r.ID})
		if e == nil {
			h := sha256.Sum256(prior.Payload)
			if !bytes.Equal(prior.Payload, raw) || !bytes.Equal(h[:], prior.PayloadDigest) || !bytes.Equal(prior.PeerFingerprint, fingerprint) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		node, e := q.LockNodeRecoveryIncarnation(ctx, c.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != c.BootID {
			return ErrStaleReceipt
		}
		h := sha256.Sum256(raw)
		if e = q.AppendNodeRecoveryCreditFenceReceipt(ctx, generated.AppendNodeRecoveryCreditFenceReceiptParams{TenantID: scope.TenantID(), FenceID: c.ID, Payload: raw, PayloadDigest: h[:], PeerFingerprint: fingerprint}); e != nil {
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
