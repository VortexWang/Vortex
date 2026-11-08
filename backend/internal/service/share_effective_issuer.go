package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/contracts"
)

type effectiveShareIssuer struct {
	Epoch       int64
	Boot        string
	Recovered   bool
	Delegations []string
}

// Caller holds the global signer lock before its tenant lock. Original share
// and closed old-epoch rows are never reopened. Successor issuance follows only
// adjacent committed admissions with original share and complete source proofs.
func effectiveShareIssuerTx(ctx context.Context, q *generated.Queries, g generated.ShareGeneration, a generated.ShareRenewalAuthority) (effectiveShareIssuer, error) {
	out := effectiveShareIssuer{Epoch: a.SignerEpoch, Delegations: []string{}}
	recovered, e := q.ShareHasAdmittedRecovery(ctx, generated.ShareHasAdmittedRecoveryParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind, Generation: g.Generation})
	if e != nil {
		return out, e
	}
	if !recovered {
		return out, nil
	}
	if g.SignerID != a.SignerID || g.SignerEpoch != a.SignerEpoch {
		return out, ErrShareUnknown
	}
	current, e := q.LatestHubIncarnation(ctx, a.SignerID)
	if e != nil {
		return out, e
	}
	if current.SignerEpoch <= a.SignerEpoch || current.SignerEpoch-a.SignerEpoch > 4096 {
		return out, ErrShareUnknown
	}
	owner, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: a.SignerID, SignerEpoch: a.SignerEpoch})
	if e != nil {
		return out, e
	}
	boot := owner.BootID
	for epoch := a.SignerEpoch; epoch < current.SignerEpoch; epoch++ {
		r, e := q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: a.SignerID, ToEpoch: epoch + 1})
		if e != nil {
			return out, ErrShareUnknown
		}
		_, c, e := sameStoreCommand(ctx, q, r.ID, a.SignerID, r.ToBootID)
		if e != nil || r.FromEpoch != epoch || r.FromBootID != boot {
			return out, ErrShareUnknown
		}
		admitted, e := q.GetHubSameStoreAdmission(ctx, r.ID)
		if e != nil {
			return out, ErrShareUnknown
		}
		proof, e := validateHubSameStoreAdmission(admitted)
		if e != nil || proof.FromEpoch != epoch || proof.ToEpoch != epoch+1 || proof.FromBootID != boot || proof.ToBootID != c.ToBootID || proof.ApprovalSHA256 != hex.EncodeToString(r.PayloadDigest) {
			return out, ErrShareUnknown
		}
		matched := 0
		for _, original := range proof.Shares {
			if original.TenantID == g.TenantID && original.NodeID == g.NodeID && original.Kind == g.Kind && original.Generation == g.Generation {
				if !jsonEqual(original, g) {
					return out, ErrShareUnknown
				}
				matched++
			}
		}
		if matched != 1 {
			return out, ErrShareUnknown
		}
		if epoch == a.SignerEpoch && (!a.ClosedAt.Valid || !bytes.Equal(a.ClosureProof, admitted.PayloadDigest)) {
			return out, ErrShareUnknown
		}
		boot = r.ToBootID
	}
	if boot != current.BootID {
		return out, ErrShareUnknown
	}
	allocations, e := q.ShareBoundHubAllocations(ctx, generated.ShareBoundHubAllocationsParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind, Generation: g.Generation, HubID: a.SignerID})
	if e != nil {
		return out, e
	}
	if len(allocations) == 0 {
		return out, ErrShareUnknown
	}
	latestRecovery, e := q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: a.SignerID, ToEpoch: current.SignerEpoch})
	if e != nil {
		return out, e
	}
	_, latestCommand, e := sameStoreCommand(ctx, q, latestRecovery.ID, a.SignerID, current.BootID)
	if e != nil {
		return out, e
	}
	transferred := map[string]bool{}
	for _, link := range latestCommand.Allocations {
		transferred[link.AllocationID] = true
	}
	bound := map[string]bool{}
	for _, original := range allocations {
		bound[original.AllocationID] = true
	}
	for _, link := range latestCommand.Allocations {
		original, e := q.GetHubAllocationGlobal(ctx, link.AllocationID)
		if e != nil {
			return out, e
		}
		if original.TenantID != g.TenantID || original.NodeID != g.NodeID {
			continue
		}
		bindings, e := q.HubAllocationShareBindings(ctx, generated.HubAllocationShareBindingsParams{TenantID: original.TenantID, AllocationID: original.AllocationID})
		if e != nil {
			return out, e
		}
		if len(bindings) != 2 {
			return out, ErrShareUnknown
		}
		matched := 0
		for _, binding := range bindings {
			if binding.Kind == g.Kind && binding.Generation == g.Generation {
				matched++
			}
		}
		if matched != 1 || !bound[original.AllocationID] {
			return out, ErrShareUnknown
		}
	}
	for _, original := range allocations {
		// A delegation sealed before admission did not transfer. Transferred
		// delegations still require every source witness after later sealing.
		if !transferred[original.AllocationID] {
			if _, e = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: original.TenantID, AllocationID: original.AllocationID}); e != nil {
				return out, ErrShareUnknown
			}
			continue
		}
		bindings, e := q.HubAllocationShareBindings(ctx, generated.HubAllocationShareBindingsParams{TenantID: original.TenantID, AllocationID: original.AllocationID})
		if e != nil {
			return out, e
		}
		if len(bindings) != 2 {
			return out, ErrShareUnknown
		}
		if _, _, e = effectiveHubAllocationTx(ctx, q, original, uint64(current.SignerEpoch), current.BootID, time.Time{}); e != nil {
			return out, e
		}
		out.Delegations = append(out.Delegations, original.DelegationID)
	}
	out.Epoch, out.Boot, out.Recovered = current.SignerEpoch, current.BootID, true
	return out, nil
}

// Exact immutable commands and authenticated FULL receipts, or a committed
// complete cold proof after every original hard deadline, close the effective
// issuer. The original old-epoch admission alone cannot close later issuers.
func (s ShareService) effectiveShareClosureTx(ctx context.Context, q *generated.Queries, g generated.ShareGeneration, handoff string, a generated.ShareRenewalAuthority) ([]byte, error) {
	if proof, found, e := s.coldShareClosureProofTx(ctx, q, g, g.Generation+1, a); e != nil || found {
		return proof, e
	}
	issuer, e := effectiveShareIssuerTx(ctx, q, g, a)
	if e != nil {
		return nil, e
	}
	if !issuer.Recovered {
		if !a.ClosedAt.Valid || len(a.ClosureProof) != 32 {
			return nil, ErrShareUnknown
		}
		return a.ClosureProof, nil
	}
	commands, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: g.TenantID, HandoffID: handoff, SignerID: a.SignerID})
	if e != nil {
		return nil, e
	}
	if len(commands) == 0 {
		return nil, ErrShareUnknown
	}
	ids := []string{}
	digests := []string{}
	for i, command := range commands {
		sum := sha256.Sum256(command.Payload)
		c, e := contracts.DecodeShareRenewalClosure(command.Payload)
		if e != nil || !bytes.Equal(sum[:], command.PayloadDigest) || c.ID != command.ID || c.HandoffID != handoff || c.TenantID != g.TenantID || c.NodeID != g.NodeID || c.BootID != g.BootID || c.HubID != a.SignerID || c.HubBootID != issuer.Boot || c.SignerEpoch != issuer.Epoch || c.Kind != g.Kind || c.Generation != g.Generation || c.SuccessorGeneration != g.Generation+1 || c.Part != int32(i+1) || c.Parts != int32(len(commands)) {
			return nil, ErrShareUnknown
		}
		receipt, e := q.GetShareClosureReceipt(ctx, generated.GetShareClosureReceiptParams{TenantID: g.TenantID, CommandID: c.ID})
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, ErrShareUnknown
		}
		if e != nil {
			return nil, e
		}
		r, e := contracts.DecodeShareRenewalClosureReceipt(receipt.Payload)
		rh := sha256.Sum256(receipt.Payload)
		if e != nil || r.ID != c.ID || r.CommandDigest != hex.EncodeToString(sum[:]) || !bytes.Equal(rh[:], receipt.PayloadDigest) || len(receipt.PeerFingerprint) == 0 {
			return nil, ErrShareUnknown
		}
		ids = append(ids, c.DelegationIDs...)
		digests = append(digests, hex.EncodeToString(rh[:]))
	}
	if !jsonEqual(ids, issuer.Delegations) {
		return nil, ErrShareUnknown
	}
	raw, e := json.Marshal(struct {
		Epoch    int64
		Boot     string
		Receipts []string
	}{issuer.Epoch, issuer.Boot, digests})
	if e != nil {
		return nil, e
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}

// Lock immutable predecessor issuers in a deterministic order before tenant
// serialization, matching admission and funding. A successor may change signer.
func lockShareIssuersTx(ctx context.Context, q *generated.Queries, g generated.ShareGeneration) error {
	signers := map[string]bool{g.SignerID: true}
	if g.Predecessor > 0 {
		authorities, e := q.ShareClosureEvidence(ctx, generated.ShareClosureEvidenceParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind, Generation: g.Predecessor})
		if e != nil {
			return e
		}
		for _, a := range authorities {
			signers[a.SignerID] = true
		}
	}
	ordered := []string{}
	for signer := range signers {
		ordered = append(ordered, signer)
	}
	sort.Strings(ordered)
	for _, signer := range ordered {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+signer); e != nil {
			return e
		}
	}
	return nil
}
