package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// RecordUnknown freezes the remaining safe reservation, never records the
// upper bound as measured traffic. Expiry, restart and lost receipts all use it.
func (s MeteringService) RecordUnknown(ctx context.Context, scope TenantScope, grant string, proof []byte, at time.Time, request string) error {
	if len(proof) != 32 || at.IsZero() {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.unknown", ResourceID: grant, RequestID: request, Reason: "unknown is frozen authority, not failed execution or measured bytes"}, func(q *generated.Queries) error {
		return recordUnknown(ctx, q, scope, grant, proof, at)
	})
}

func recordUnknown(ctx context.Context, q *generated.Queries, scope TenantScope, grant string, proof []byte, at time.Time) error {
	r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: grant})
	if e != nil {
		return e
	}
	if _, e = q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: r.SourceID}); e != nil {
		return e
	}
	if _, e = q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: grant}); e == nil {
		return nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: grant})
	if e != nil {
		return e
	}
	if a.Terminal {
		return ErrConflict
	}
	raw, bill := r.RawGranted-a.RawCumulative-a.ReturnedRaw, r.ReservedBill-a.Charged-a.ReleasedBill
	if e = q.AppendUnknownLiability(ctx, generated.AppendUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: grant, BillingSnapshotID: r.BillingSnapshotID, ConfirmedRaw: a.RawCumulative, UnknownRaw: raw, UnknownBill: bill, Evidence: auditJSON(map[string]any{"proof": proof, "node_id": r.NodeID, "boot_id": r.BootID}), CreatedAt: timestamp(at)}); e != nil {
		return e
	}
	if e = ledgerEvent(ctx, q, scope, r.SourceID, r.BillingSnapshotID, "unknown/"+grant, "unknown", 0, 0, 0, 0, 0, map[string]any{"grant_id": grant, "confirmed_raw": a.RawCumulative, "unknown_raw": raw, "unknown_bill": bill, "proof": proof}); e != nil {
		return e
	}
	task, e := newDomainTask(ctx, q, scope, grant, "usage.unknown", "usage_reconcile", 1)
	if e != nil {
		return e
	}
	return q.AppendUsageReconciliationJob(ctx, generated.AppendUsageReconciliationJobParams{TenantID: scope.TenantID(), GrantID: grant, TaskID: task.ID, NextAttempt: timestamp(at)})
}

type RiskFunding struct {
	ID, BackingReference, Approval string
	Amount                         int64
	ValidUntil                     time.Time
}

func (s MeteringService) FundRiskBudget(ctx context.Context, scope TenantScope, f RiskFunding, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) || f.Amount < 1 || f.BackingReference == "" || f.Approval == "" || !f.ValidUntil.After(time.Now()) {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "risk.fund", ResourceID: f.ID, RequestID: request, Reason: "explicit approved platform backing; no default or automatic budget"}, func(q *generated.Queries) error {
		return q.FundRiskBudget(ctx, generated.FundRiskBudgetParams{ID: f.ID, Amount: f.Amount, BackingReference: f.BackingReference, ApprovedBy: scope.ActorID(), ApprovalEvidence: auditJSON(map[string]any{"approval": f.Approval}), ValidUntil: timestamp(f.ValidUntil)})
	})
}

func (s MeteringService) ConservativeClose(ctx context.Context, scope TenantScope, grant, budget string, at time.Time, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return ErrScope
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "risk.conservative_close", ResourceID: grant, RequestID: request, Reason: "72h funded upper-bound settlement and new-source compensation in one transaction"}, func(q *generated.Queries) error {
		// Budget then source is the same lock order for every closure.
		funding, e := q.LockRiskBudget(ctx, budget)
		if e != nil {
			return e
		}
		if !at.Before(funding.ValidUntil.Time) {
			return ErrConflict
		}
		used, e := q.RiskBudgetUsed(ctx, budget)
		if e != nil {
			return e
		}
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: grant})
		if e != nil {
			return e
		}
		p, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: r.SourceID})
		if e != nil {
			return e
		}
		unknown, e := q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: grant})
		if e != nil {
			return e
		}
		if at.Before(unknown.CreatedAt.Time.Add(72 * time.Hour)) {
			return ErrConflict
		}
		a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: grant})
		if e != nil {
			return e
		}
		if a.Terminal {
			_, e = q.GrantRiskDebit(ctx, generated.GrantRiskDebitParams{TenantID: scope.TenantID(), GrantID: grant})
			return e
		}
		// Known consumption may have advanced since fault detection. Close only the
		// currently outstanding safe upper bound, not the obsolete initial bound.
		upper := r.ReservedBill - a.Charged - a.ReleasedBill
		if upper <= 0 || upper > funding.Amount-used {
			return ErrConflict
		}
		sub, e := q.GetSubscription(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		compensation := uuid.NewString()
		if _, e = q.AppendTrafficSource(ctx, generated.AppendTrafficSourceParams{ID: compensation, TenantID: scope.TenantID(), SnapshotID: sub.SnapshotID, Kind: "traffic", InitialAmount: upper, IssuedAt: timestamp(at), ExpiresAt: funding.ValidUntil, PeriodKey: "risk/" + grant}); e != nil {
			return e
		}
		if e = q.DebitRiskBudget(ctx, generated.DebitRiskBudgetParams{BudgetID: budget, TenantID: scope.TenantID(), GrantID: grant, Amount: upper, CompensationSourceID: compensation, Evidence: auditJSON(map[string]any{"approved_by": funding.ApprovedBy, "backing_reference": funding.BackingReference})}); e != nil {
			return e
		}
		if e = q.EnsureSourceLedger(ctx, generated.EnsureSourceLedgerParams{TenantID: scope.TenantID(), ID: compensation}); e != nil {
			return e
		}
		if e = q.EnsureLedgerProjection(ctx, generated.EnsureLedgerProjectionParams{TenantID: scope.TenantID(), SourceID: compensation}); e != nil {
			return e
		}
		if e = ledgerEvent(ctx, q, scope, compensation, r.BillingSnapshotID, "risk-compensation/"+grant, "compensation", upper, 0, 0, 0, 0, map[string]any{"budget_id": budget, "backing_reference": funding.BackingReference, "original_source": r.SourceID, "grant_id": grant}); e != nil {
			return e
		}
		if e = q.AppendUsageSettlement(ctx, generated.AppendUsageSettlementParams{TenantID: scope.TenantID(), GrantID: grant, BillingSnapshotID: r.BillingSnapshotID, Kind: "conservative", ReceiptKey: "conservative", RawCumulative: a.RawCumulative, BillableNumerator: numerator(a.RawCumulative, r.CoefficientMilli), ChargedDelta: upper, Evidence: auditJSON(map[string]any{"unknown_bill": upper, "compensation_source_id": compensation, "budget_id": budget})}); e != nil {
			return e
		}
		b := min(p.B, upper)
		l := upper - b
		return ledgerEvent(ctx, q, scope, r.SourceID, r.BillingSnapshotID, "conservative/"+grant, "conservative", -b, -upper, -l, upper, 0, map[string]any{"not_measured": true, "unknown_bill": upper, "compensation_source_id": compensation, "budget_id": budget})
	})
}

func (s MeteringService) ReconcileExact(ctx context.Context, scope TenantScope, grant string, raw int64, proof []byte, request string) error {
	if len(proof) != 32 || raw < 0 {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.reconcile_exact", ResourceID: grant, RequestID: request, Reason: "late source-local exact proof; never duplicate user funds or charges"}, func(q *generated.Queries) error {
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: grant})
		if e != nil {
			return e
		}
		if _, e = q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: r.SourceID}); e != nil {
			return e
		}
		u, e := q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: grant})
		if e != nil {
			return e
		}
		if raw < u.ConfirmedRaw || raw > u.ConfirmedRaw+u.UnknownRaw {
			return ErrConflict
		}
		a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: grant})
		if e != nil {
			return e
		}
		debit, e := q.GrantRiskDebit(ctx, generated.GrantRiskDebitParams{TenantID: scope.TenantID(), GrantID: grant})
		if errors.Is(e, pgx.ErrNoRows) {
			return settleActual(ctx, q, scope, r, "final", "exact-final", raw, 0, proof)
		}
		if e != nil {
			return e
		}
		old, e := q.GetSettlementReceipt(ctx, generated.GetSettlementReceiptParams{TenantID: scope.TenantID(), GrantID: grant, ReceiptKey: "late-exact"})
		if e == nil {
			if old.RawCumulative != raw {
				return ErrUsageDisputed
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if raw < a.RawCumulative {
			return ErrConflict
		}
		exactCost, e := amount(raw-a.RawCumulative, r.CoefficientMilli, true)
		if e != nil || exactCost > debit.Amount {
			return ErrConflict
		}
		saved := debit.Amount - exactCost
		if e = q.RecoverRiskBudget(ctx, generated.RecoverRiskBudgetParams{TenantID: scope.TenantID(), GrantID: grant, Amount: saved, Evidence: auditJSON(map[string]any{"proof": proof, "budget_id": debit.BudgetID, "raw_exact": raw})}); e != nil {
			return e
		}
		if e = q.AppendUsageSettlement(ctx, generated.AppendUsageSettlementParams{TenantID: scope.TenantID(), GrantID: grant, BillingSnapshotID: r.BillingSnapshotID, Kind: "late_exact", ReceiptKey: "late-exact", RawCumulative: raw, BillableNumerator: numerator(raw, r.CoefficientMilli), Evidence: auditJSON(map[string]any{"proof": proof, "platform_recovery": saved})}); e != nil {
			return e
		}
		return ledgerEvent(ctx, q, scope, r.SourceID, r.BillingSnapshotID, "late-exact/"+grant, "late_exact", 0, 0, 0, 0, 0, map[string]any{"raw_exact": raw, "platform_recovery": saved, "proof": proof})
	})
}

// ProtectionState describes only an already-preauthorized policy handover.
// It never extends a runtime/share/autonomy deadline or authorizes writes.
func ProtectionState(lo, hi, boundary, started, hardDeadline time.Time, preauthorized bool) string {
	if lo.IsZero() || hi.Before(lo) || hi.Sub(lo) > 12*time.Second || !hi.Before(hardDeadline) {
		return "close"
	}
	if !hi.Before(boundary) && lo.Before(boundary) {
		if !preauthorized || started.IsZero() || hi.Sub(started) > 12*time.Second {
			return "close"
		}
		return "pause"
	}
	if !lo.Before(boundary) {
		if !preauthorized {
			return "close"
		}
		return "new"
	}
	return "old"
}

// ReconcilePending schedules bounded retries and a durable alert; neither 24h
// nor 72h is a permission to release a reservation without evidence or funding.
func (s MeteringService) ReconcilePending(ctx context.Context, tenant, grant string, at time.Time) error {
	scope, e := NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/usage-reconcile/"+tenant)).String(), tenant, tenant)
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.reconcile_pending", ResourceID: grant, RequestID: uuid.NewString(), Reason: "bounded retry; unproven and unfunded liability remains frozen"}, func(q *generated.Queries) error {
		j, e := q.LockUsageReconciliation(ctx, generated.LockUsageReconciliationParams{TenantID: tenant, GrantID: grant})
		if e != nil {
			return e
		}
		a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: tenant, GrantID: grant})
		if e != nil {
			return e
		}
		if a.Terminal {
			if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: tenant, OperationID: j.TaskID, State: "succeeded"}); e != nil {
				return e
			}
			if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: j.TaskID, State: "succeeded"}); e != nil {
				return e
			}
			return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: tenant, OperationID: j.TaskID})
		}
		if at.Before(j.NextAttempt.Time) {
			return nil
		}
		u, e := q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: tenant, GrantID: grant})
		if e != nil {
			return e
		}
		alert := !at.Before(u.CreatedAt.Time.Add(72 * time.Hour))
		if alert && !j.AlertedAt.Valid {
			if e = dispute(ctx, q, scope, generated.Reservation{ID: grant, BillingSnapshotID: u.BillingSnapshotID}, "72h unresolved; approved funded risk budget required", map[string]any{"unknown_bill": u.UnknownBill, "grant_id": grant}); e != nil {
				return e
			}
		}
		delay := time.Second * time.Duration(1<<min(j.Attempts, 12))
		if delay > time.Hour {
			delay = time.Hour
		}
		return q.BackoffUsageReconciliation(ctx, generated.BackoffUsageReconciliationParams{TenantID: tenant, GrantID: grant, NextAttempt: timestamp(at.Add(delay)), Column4: alert, Column5: timestamp(at)})
	})
}

func (s MeteringService) DetectExpiredUsage(ctx context.Context, at time.Time) (bool, error) {
	rows, e := generated.New(s.Pool).ExpiredUsageReservations(ctx, timestamp(at))
	if e != nil || len(rows) == 0 {
		return false, e
	}
	r := rows[0]
	scope, e := NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/usage-expiry/"+r.TenantID)).String(), r.TenantID, r.TenantID)
	if e != nil {
		return false, e
	}
	proof := sha256.Sum256([]byte(fmt.Sprintf("expired receipt missing/%s/%s/%s", r.ID, r.NodeID, r.BootID)))
	e = s.RecordUnknown(ctx, scope, r.ID, proof[:], at, uuid.NewString())
	return e == nil, e
}
