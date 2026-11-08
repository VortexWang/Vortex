package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
)

type hubCreditFinalProof struct {
	Version      int    `json:"version"`
	AllocationID string `json:"allocation_id"`
	SealSHA256   string `json:"seal_sha256"`
	ChildCount   int    `json:"child_count"`
	ChildSHA256  string `json:"child_sha256"`
	Used         int64  `json:"used"`
	Unused       int64  `json:"unused"`
}

// FinalizeHubAllocation settles the original single-source parent only after
// every issued child has an authenticated FULL final, an independently matched
// known wire inventory and no outstanding unknown/disputed liability. Original
// coefficient and source remainder apply once to aggregate actual usage.
func (s ShareService) FinalizeHubAllocation(ctx context.Context, scope TenantScope, allocation, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.credit_final", ResourceID: allocation, RequestID: request, Reason: "complete sealed issuer and authenticated child inventories prove final original-source usage; no expiry or generic proof substitution"}, func(q *generated.Queries) error {
		if _, err := q.LockTenant(ctx, scope.TenantID()); err != nil {
			return err
		}
		a, err := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if err != nil {
			return err
		}
		sealed, err := q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrShareUnknown
		} else if err != nil {
			return err
		}
		seal, err := contracts.DecodeDelegationSeal(sealed.Payload)
		sum := sha256.Sum256(sealed.Payload)
		if err != nil || !bytes.Equal(sum[:], sealed.PayloadDigest) || seal.DelegationID != a.DelegationID || seal.NodeID != a.NodeID || seal.BootID != a.BootID || seal.HubID != a.HubID || seal.HubBootID != a.HubBootID || len(sealed.PeerFingerprint) != 32 {
			return ErrShareConflict
		}
		// Match ordinary and unknown ingestion before inspecting inventories.
		if err = q.LockUsageTuple(ctx, a.NodeID+"/"+a.BootID); err != nil {
			return err
		}
		if err = q.LockUsageTuple(ctx, "funded-usage/"+scope.TenantID()); err != nil {
			return err
		}
		parent, err := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: a.ReservationID})
		if err != nil {
			return err
		}
		if _, err = q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: parent.SourceID}); err != nil {
			return err
		}
		if _, err = q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: parent.ID}); err == nil {
			return ErrShareUnknown
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		disputed, err := q.ReservationDisputed(ctx, generated.ReservationDisputedParams{TenantID: scope.TenantID(), GrantID: parent.ID})
		if err != nil {
			return err
		}
		if disputed {
			return ErrShareUnknown
		}
		issued, err := q.HubAllocationIssueInventory(ctx, generated.HubAllocationIssueInventoryParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if err != nil {
			return err
		}
		var original []contracts.IssuedPartition
		for _, row := range issued {
			original = append(original, contracts.IssuedPartition{ID: row.IssueID, Digest: hex.EncodeToString(row.PayloadDigest), Raw: row.RawGranted})
		}
		digest, issuedRaw, err := contracts.PartitionDigest(original)
		if err != nil || digest != seal.IssueDigest || issuedRaw != seal.Issued || int64(len(issued)) != seal.IssueCount || seal.Raw != parent.RawGranted {
			return ErrShareUnknown
		}
		children, err := q.HubAllocationChildren(ctx, generated.HubAllocationChildrenParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if err != nil {
			return err
		}
		if len(children) != len(issued) {
			return ErrShareUnknown
		}
		var actual int64
		var finals []contracts.IssuedPartition
		for _, child := range children {
			p, err := q.GetCreditCloseCommand(ctx, generated.GetCreditCloseCommandParams{TenantID: scope.TenantID(), GrantID: child.GrantID})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrShareUnknown
			} else if err != nil {
				return err
			}
			r, err := q.GetCreditCloseReceipt(ctx, generated.GetCreditCloseReceiptParams{TenantID: scope.TenantID(), GrantID: child.GrantID})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrShareUnknown
			} else if err != nil {
				return err
			}
			rsum := sha256.Sum256(r.Payload)
			if !bytes.Equal(rsum[:], r.PayloadDigest) || len(r.PeerFingerprint) != 32 || p.AllocationID != allocation || p.NodeID != a.NodeID || p.BootID != a.BootID {
				return ErrShareConflict
			}
			command, final, err := validateCreditCloseFinal(p, r.Payload)
			if err != nil {
				return err
			}
			if final.Grant.AmountBytes != uint64(child.RawGranted) || final.Used > uint64(seal.Issued-actual) || command.HubID != a.HubID {
				return ErrShareConflict
			}
			if err = verifyChildKnownUsage(ctx, q, scope.TenantID(), command, final); err != nil {
				return err
			}
			actual += int64(final.Used)
			finals = append(finals, contracts.IssuedPartition{ID: child.GrantID, Digest: hex.EncodeToString(rsum[:]), Raw: child.RawGranted})
		}
		finalDigest, total, err := contracts.PartitionDigest(finals)
		if err != nil || total != seal.Issued {
			return ErrShareUnknown
		}
		accounting, err := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: parent.ID})
		if err != nil {
			return err
		}
		if accounting.RawCumulative != actual || accounting.ReturnedRaw != seal.Unissued {
			return ErrShareUnknown
		}
		if seal.Issued == 0 {
			if accounting.Terminal {
				return nil // accepted zero-child seal already finalized the parent
			}
			return ErrShareUnknown
		}
		proof := hubCreditFinalProof{Version: 1, AllocationID: allocation, SealSHA256: hex.EncodeToString(sum[:]), ChildCount: len(children), ChildSHA256: finalDigest, Used: actual, Unused: parent.RawGranted - actual}
		key := "hub-credit-final/" + allocation
		_, oldErr := q.GetSettlementReceipt(ctx, generated.GetSettlementReceiptParams{TenantID: scope.TenantID(), GrantID: parent.ID, ReceiptKey: key})
		if oldErr != nil && !errors.Is(oldErr, pgx.ErrNoRows) {
			return oldErr
		}
		if err = settleActual(ctx, q, scope, parent, "final", key, actual, 0, proof); err != nil {
			return err
		}
		if oldErr == nil {
			return nil
		}
		return domainTask(ctx, q, scope, allocation, "hub.credit_final", "resource_change", 1)
	})
}
