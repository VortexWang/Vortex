package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/hub"
)

// effectiveHubAllocationTx derives authority from the immutable original and
// every adjacent, committed admission. An epoch column or a retained manifest
// alone cannot replace the original source, root limits or financial parent.
// Caller holds the global Hub lock before the tenant lock.
func effectiveHubAllocationTx(ctx context.Context, q *generated.Queries, original generated.HubFundedAllocation, epoch uint64, boot string, issuedAt time.Time) (hub.Allocation, []store.EpochHandoff, error) {
	a, e := hub.DecodeAllocation(original.Payload)
	hash := sha256.Sum256(original.Payload)
	if e != nil || !bytes.Equal(hash[:], original.PayloadDigest) || a.AllocationID != original.AllocationID || a.DelegationID != original.DelegationID || a.SourceID != original.ReservationID || a.NodeID != original.NodeID || a.BootID != original.BootID || a.HubID != original.HubID {
		return a, nil, ErrShareConflict
	}
	if epoch < a.SignerEpoch || epoch > math.MaxInt64 || epoch-a.SignerEpoch > 4096 {
		return a, nil, ErrShareUnknown
	}
	delegation, e := json.Marshal(a.Delegation())
	if e != nil {
		return a, nil, e
	}
	dh := sha256.Sum256(delegation)
	owner := original.HubBootID
	var handoffs []store.EpochHandoff
	for a.SignerEpoch < epoch {
		r, e := q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: a.HubID, ToEpoch: int64(a.SignerEpoch + 1)})
		if e != nil {
			return a, nil, ErrShareUnknown
		}
		_, c, e := sameStoreCommand(ctx, q, r.ID, a.HubID, r.ToBootID)
		if e != nil || r.FromBootID != owner || c.FromBootID != owner || c.BackendID != r.BackendID || r.FromEpoch != int64(a.SignerEpoch) || c.Handoff.FromEpoch != a.SignerEpoch {
			return a, nil, ErrShareConflict
		}
		admitted, e := q.GetHubSameStoreAdmission(ctx, r.ID)
		if e != nil {
			return a, nil, ErrShareUnknown
		}
		proof, e := validateHubSameStoreAdmission(admitted)
		if e != nil || proof.FromEpoch != r.FromEpoch || proof.ToEpoch != r.ToEpoch || proof.FromBootID != owner || proof.ToBootID != c.ToBootID || proof.BackendID != c.BackendID || proof.ApprovalSHA256 != hex.EncodeToString(r.PayloadDigest) {
			return a, nil, ErrShareConflict
		}
		if !issuedAt.IsZero() && issuedAt.Before(proof.Earliest) {
			return a, nil, ErrLeaseBoundary
		}
		retained, e := q.GetHubSameStoreManifest(ctx, r.ID)
		if e != nil {
			return a, nil, e
		}
		mh := sha256.Sum256(retained.Payload)
		m, e := hub.DecodeSameStoreManifest(retained.Payload, c)
		if e != nil || !bytes.Equal(mh[:], retained.PayloadDigest) || proof.ManifestSHA256 != hex.EncodeToString(mh[:]) || retained.SealedSequence != m.Result.SealedSequence {
			return a, nil, ErrShareConflict
		}
		index := -1
		for i, link := range c.Allocations {
			if link.AllocationID == a.AllocationID {
				if link.DelegationID != a.DelegationID || link.PayloadSHA256 != hex.EncodeToString(hash[:]) || link.DelegationSHA256 != hex.EncodeToString(dh[:]) {
					return a, nil, ErrShareConflict
				}
				index = i
			}
		}
		if index < 0 {
			return a, nil, ErrShareUnknown
		}
		next := c.Handoff.FundedSuccessors[index].ShareReferences
		for i, old := range a.ShareReferences {
			if old.Revision >= math.MaxInt64 {
				return a, nil, ErrShareUnknown
			}
			old.Revision++
			old.SignerEpoch++
			if !jsonEqual(old, next[i]) {
				return a, nil, ErrShareConflict
			}
		}
		w, e := recheckRecoveryNodeWitness(ctx, q, original, c, m, index)
		if e != nil {
			return a, nil, e
		}
		witnesses := 0
		for _, recorded := range proof.Nodes {
			if recorded.AllocationID == a.AllocationID {
				if recorded != w {
					return a, nil, ErrShareConflict
				}
				witnesses++
			}
		}
		if witnesses != 1 || m.Result.FundedInventory.Delegations[index].Raw != int64(a.RawBytes) {
			return a, nil, ErrShareConflict
		}
		h := store.EpochHandoff{TransferID: r.ID, ParentID: a.DelegationID, SourceID: a.SourceID, From: a.SignerEpoch, To: a.SignerEpoch + 1, Remaining: m.Result.Remaining[a.DelegationID], SealedSequence: m.Result.SealedSequence, RecordSHA256: hex.EncodeToString(mh[:])}
		handoffs = []store.EpochHandoff{h}
		for _, source := range []string{a.RuntimeSourceID, a.ShareSourceID} {
			h.SourceID, h.Remaining = source, 0
			handoffs = append(handoffs, h)
		}
		a.SignerEpoch++
		a.ShareReferences = append([]store.ShareReference(nil), next...)
		owner = c.ToBootID
	}
	if owner != boot {
		return a, nil, ErrScope
	}
	return a, handoffs, nil
}

// New records require the currently bound incarnation. Exact historical replay
// is checked separately before this gate, without granting fresh authority.
func currentHubEpochTx(ctx context.Context, q *generated.Queries, hub, boot string, epoch uint64) error {
	current, e := q.LatestHubIncarnation(ctx, hub)
	if e != nil {
		return e
	}
	if current.BootID != boot || uint64(current.SignerEpoch) != epoch {
		return ErrShareUnknown
	}
	return nil
}
