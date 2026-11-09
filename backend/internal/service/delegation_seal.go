package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
)

// RecordDelegationSeal returns only the exact never-issued remainder after the
// complete Hub issue inventory has independently reached PostgreSQL. Issued
// children keep their original hold and unknown liability, without per-child
// rounding or an invented final Node receipt.
func (s ShareService) RecordDelegationSeal(ctx context.Context, hub, boot string, sequence int64, raw []byte, request string) error {
	if sequence < 1 {
		return ErrShareConflict
	}
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hub)
	if e != nil {
		return e
	}
	seal, e := contracts.DecodeDelegationSeal(raw)
	if e != nil {
		return ErrShareConflict
	}
	if seal.HubID != hub || seal.HubBootID != boot {
		return ErrShareConflict
	}
	a, e := generated.New(s.Pool).HubAllocationByDelegation(ctx, seal.DelegationID)
	if e != nil {
		return e
	}
	if a.HubID != hub || a.NodeID != seal.NodeID || a.BootID != seal.BootID {
		return ErrShareConflict
	}
	scope, e := hubScope(hub, a.TenantID)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.delegation_sealed", ResourceID: seal.DelegationID, RequestID: request, Reason: "complete immutable issuer inventory proves unissued remainder; issued children and unknown debt remain held"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hub); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		if e == nil {
			if old.LedgerSequence != sequence || !bytes.Equal(old.Payload, raw) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = currentHubEpochTx(ctx, q, hub, boot, uint64(seal.SignerEpoch)); e != nil {
			return e
		}
		reservation, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: a.ReservationID})
		if e != nil {
			return e
		}
		allocation, _, e := effectiveHubAllocationTx(ctx, q, a, uint64(seal.SignerEpoch), boot, time.Time{})
		if e != nil {
			return e
		}
		if uint64(seal.SignerEpoch) != allocation.SignerEpoch {
			return ErrShareConflict
		}
		if seal.Raw != reservation.RawGranted || seal.SignerEpoch < 1 {
			return ErrShareConflict
		}
		rows, e := q.HubAllocationIssueInventory(ctx, generated.HubAllocationIssueInventoryParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID})
		if e != nil {
			return e
		}
		inventory := make([]contracts.IssuedPartition, 0, len(rows))
		for _, r := range rows {
			inventory = append(inventory, contracts.IssuedPartition{ID: r.IssueID, Digest: hex.EncodeToString(r.PayloadDigest), Raw: r.RawGranted})
		}
		digest, total, e := contracts.PartitionDigest(inventory)
		if e != nil {
			return e
		}
		if int64(len(rows)) != seal.IssueCount || total != seal.Issued || digest != seal.IssueDigest {
			return ErrShareUnknown
		}
		accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: reservation.ID})
		if e != nil {
			return e
		}
		if accounting.Terminal || accounting.ReturnedRaw != 0 || accounting.RawCumulative > seal.Issued {
			return ErrShareConflict
		}
		if e = q.AppendHubDelegationSeal(ctx, generated.AppendHubDelegationSealParams{TenantID: scope.TenantID(), AllocationID: a.AllocationID, SealID: seal.ID, HubID: hub, HubBootID: boot, LedgerSequence: sequence, Payload: raw, PayloadDigest: sum[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		// With issued children, return preserves ceil(issued * coefficient) less
		// already charged usage. With no children, final is safe because none could
		// ever have spent; an authenticated issuer seal proves that zero inventory.
		kind := "return"
		if seal.Issued == 0 {
			kind = "final"
		}
		if e = settleActual(ctx, q, scope, reservation, kind, "hub-seal/"+seal.ID, accounting.RawCumulative, seal.Unissued, seal); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, seal.DelegationID, "hub.delegation_sealed", "resource_change", sequence)
	})
}
