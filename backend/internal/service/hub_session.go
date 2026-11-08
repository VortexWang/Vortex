package service

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type HubSession struct {
	Epoch                      int64
	Earliest, Latest, Deadline time.Time
}

// BindHubSession can bind an already approved epoch, never create one. Until
// recovery supplies durable closure evidence, every other boot stays blocked,
// including a boot presenting a newly approved epoch. No held funds move here.
func (s ShareService) BindHubSession(ctx context.Context, hubID, bootID, request string) (HubSession, error) {
	var out HubSession
	if !validID(bootID) {
		return out, ErrShareConflict
	}
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID); e != nil {
		return out, e
	}
	b, e := generated.New(s.Pool).HubSignerBoundary(ctx, hubID)
	if e != nil {
		return out, e
	}
	scope, e := hubScope(hubID, b.TenantID)
	if e != nil {
		return out, e
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.bind_boot", ResourceID: hubID, RequestID: request, Reason: "authenticated boot bound to preapproved epoch; unknown prior rights remain held"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hubID); e != nil {
			return e
		}
		current, e := q.HubSignerBoundary(ctx, hubID)
		if e != nil {
			return e
		}
		if current.TenantID != b.TenantID || current.SignerEpoch != b.SignerEpoch {
			return ErrShareConflict
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		if !hi.Before(current.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		previous, e := q.LatestHubIncarnation(ctx, hubID)
		if e == nil {
			if previous.BootID != bootID || previous.SignerEpoch != current.SignerEpoch {
				return ErrShareUnknown
			}
		} else if errors.Is(e, pgx.ErrNoRows) {
			if e = q.BindHubIncarnation(ctx, generated.BindHubIncarnationParams{HubID: hubID, SignerEpoch: current.SignerEpoch, BootID: bootID}); e != nil {
				return e
			}
			if e = domainTask(ctx, q, scope, hubID, "hub.boot_bound", "resource_change", current.SignerEpoch); e != nil {
				return e
			}
		} else {
			return e
		}
		out = HubSession{Epoch: current.SignerEpoch, Earliest: lo, Latest: hi, Deadline: current.AutonomyUntil.Time.UTC()}
		return nil
	})
	if e != nil {
		return HubSession{}, e
	}
	return out, nil
}

func hubScope(hubID, tenant string) (TenantScope, error) {
	return NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/hub/"+hubID)).String(), tenant, tenant)
}

// PendingHubAllocations reads committed funding only. Transport still binds the
// outer authenticated boot; an installed receipt never means Node activation.
func (s ShareService) PendingHubAllocations(ctx context.Context, hubID, bootID string) ([]generated.HubFundedAllocation, error) {
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID); e != nil {
		return nil, e
	}
	return generated.New(s.Pool).PendingHubAllocations(ctx, generated.PendingHubAllocationsParams{HubID: hubID, HubBootID: bootID})
}

func (s ShareService) RecordHubAllocationReceipt(ctx context.Context, hubID, bootID, id string, digest []byte, request string) error {
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID); e != nil {
		return e
	}
	a, e := generated.New(s.Pool).GetHubAllocationGlobal(ctx, id)
	if e != nil {
		return e
	}
	if a.HubID != hubID || a.HubBootID != bootID || !bytes.Equal(a.PayloadDigest, digest) {
		return ErrShareConflict
	}
	scope, e := hubScope(hubID, a.TenantID)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "hub.allocation_installed", ResourceID: id, RequestID: request, Reason: "authenticated FULL allocation receipt; no Node activation or money release implied"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, a.TenantID); e != nil {
			return e
		}
		old, e := q.GetHubAllocationReceipt(ctx, generated.GetHubAllocationReceiptParams{TenantID: a.TenantID, AllocationID: id})
		if e == nil {
			if old.HubID != hubID || old.HubBootID != bootID || !bytes.Equal(old.PayloadDigest, digest) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = q.AppendHubAllocationReceipt(ctx, generated.AppendHubAllocationReceiptParams{TenantID: a.TenantID, AllocationID: id, HubID: hubID, HubBootID: bootID, PayloadDigest: digest}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: a.TenantID, OperationID: a.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: a.TenantID, ID: a.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: a.TenantID, OperationID: a.OperationID}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, id, "hub.allocation_installed", "resource_change", 1)
	})
}
