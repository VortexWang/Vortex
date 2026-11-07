package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"math/big"
	"sort"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

var ErrUsageDisputed = errors.New("usage disputed; original reservation retained")

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

type MeteringService struct{ Pool *pgxpool.Pool }
type ReserveInput struct {
	SourceID, SnapshotID, NodeID, BootID, Nonce string
	RawBytes                                    int64
	Earliest, Latest, Deadline                  time.Time
	Preauthorize                                bool
}

func amount(n, k int64, ceil bool) (int64, error) {
	if n < 0 || k < 0 {
		return 0, ErrConflict
	}
	x := new(big.Int).Mul(big.NewInt(n), big.NewInt(k))
	if ceil {
		x.Add(x, big.NewInt(999))
	}
	x.Div(x, big.NewInt(1000))
	if !x.IsInt64() {
		return 0, ErrConflict
	}
	return x.Int64(), nil
}
func numerator(n, k int64) pgtype.Numeric {
	return pgtype.Numeric{Int: new(big.Int).Mul(big.NewInt(n), big.NewInt(k)), Valid: true}
}
func finiteTimeBefore(at time.Time, b pgtype.Timestamptz) bool {
	return b.InfinityModifier == pgtype.Infinity || (b.Valid && at.Before(b.Time))
}
func timeAtOrAfter(at time.Time, b pgtype.Timestamptz) bool {
	return b.InfinityModifier == pgtype.NegativeInfinity || (b.Valid && !at.Before(b.Time))
}

// ReserveUsage only funds an immutable PostgreSQL hold. It does not invent the
// signer epoch, runtime lease or Hub delegation supplied by plan 06-06.
func (s MeteringService) ReserveUsage(ctx context.Context, scope TenantScope, in ReserveInput, request string) (generated.Reservation, error) {
	var out generated.Reservation
	if in.RawBytes < 1 || in.Nonce == "" || in.Earliest.IsZero() || in.Latest.Before(in.Earliest) || in.Latest.Sub(in.Earliest) > 12*time.Second || !in.Deadline.After(in.Latest) {
		return out, ErrConflict
	}
	if _, e := uuid.Parse(in.NodeID); e != nil {
		return out, e
	}
	if _, e := uuid.Parse(in.BootID); e != nil {
		return out, e
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.reserve", ResourceID: in.SourceID, RequestID: request, Reason: "source-local funded hold; no lease or signer invented"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, in.NodeID+"/"+in.BootID+"/"+in.Nonce); e != nil {
			return e
		}
		old, e := q.FindUsageReservation(ctx, generated.FindUsageReservationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID, RequestNonce: in.Nonce})
		if e == nil {
			if old.SourceID != in.SourceID || old.BillingSnapshotID != in.SnapshotID || old.RawGranted != in.RawBytes || !old.ValidUntil.Time.Equal(in.Deadline) {
				return ErrConflict
			}
			out = old
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		source, e := q.MeteringSource(ctx, generated.MeteringSourceParams{TenantID: scope.TenantID(), ID: in.SourceID, AtTime: timestamp(in.Latest)})
		if e != nil {
			return e
		}
		if source.Expired || in.Earliest.Before(source.IssuedAt.Time) || !in.Latest.Before(source.EffectiveUntil.Time) {
			return ErrConflict
		}
		if e = activeRuleSource(ctx, q, scope); e != nil {
			return e
		}
		b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: in.SnapshotID})
		if e != nil {
			return e
		}
		until, e := q.SnapshotEffectiveUntil(ctx, generated.SnapshotEffectiveUntilParams{TenantID: scope.TenantID(), ID: in.SnapshotID})
		if e != nil {
			return e
		}
		if b.MultiplierMilli == 0 || source.SnapshotID != b.PackageSnapshotID || !finiteTimeBefore(in.Latest, until) {
			return ErrConflict
		}
		if b.ValidFrom.InfinityModifier == pgtype.Finite && in.Earliest.Before(b.ValidFrom.Time) && !in.Latest.Before(b.ValidFrom.Time) {
			return ErrConflict
		}
		if !timeAtOrAfter(in.Earliest, b.ValidFrom) && (!in.Preauthorize || !b.AnnouncedAt.Valid || in.Deadline.Before(b.ValidFrom.Time.Add(12*time.Second))) {
			return ErrConflict
		}
		p, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: in.SourceID})
		if e != nil {
			return e
		}
		reserved, e := amount(in.RawBytes, b.MultiplierMilli, true)
		if e != nil || reserved > p.B-p.F {
			return ErrConflict
		}
		out, e = q.AppendUsageReservation(ctx, generated.AppendUsageReservationParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SourceID: in.SourceID, BillingSnapshotID: in.SnapshotID, NodeID: in.NodeID, BootID: in.BootID, RequestNonce: in.Nonce, RawGranted: in.RawBytes, CoefficientMilli: b.MultiplierMilli, ReservedBill: reserved, ValidUntil: timestamp(in.Deadline)})
		if e != nil {
			return e
		}
		return ledgerEvent(ctx, q, scope, in.SourceID, in.SnapshotID, "reserve/"+out.ID, "reserve", 0, reserved, 0, 0, 0, map[string]any{"grant_id": out.ID, "raw_granted": in.RawBytes, "coefficient_milli": b.MultiplierMilli})
	})
	return out, e
}
func activeRuleSource(ctx context.Context, q *generated.Queries, scope TenantScope) error {
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	if !time.Now().Before(sub.ExpiresAt.Time) {
		return ErrConflict
	}
	// Source availability is checked independently; account and package
	// restrictions never become bypassable by a fresh nonce or a cache hit.
	restrictions, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(time.Now())})
	if e != nil {
		return e
	}
	for _, r := range restrictions {
		if !r.ResourceID.Valid {
			return ErrConflict
		}
	}
	return nil
}

type UsageFact struct {
	GrantID       string `json:"grant_id"`
	RawCumulative int64  `json:"raw_cumulative"`
	Role          string `json:"role"`
}
type UsageInput struct {
	NodeID, BootID, BatchID, SnapshotID string
	Sequence                            int64
	Payload, Digest                     []byte
	Facts                               []UsageFact
	Delta                               bool // existing Agent wire reports deltas; apply only after tuple replay check
}
type UsageAck struct {
	BatchID string `json:"batch_id"`
	Digest  []byte `json:"payload_sha256"`
	State   string `json:"state"`
}

func dispute(ctx context.Context, q *generated.Queries, scope TenantScope, r generated.Reservation, reason string, evidence any) error {
	raw := auditJSON(evidence)
	digest := sha256.Sum256(raw)
	return q.AppendUsageDispute(ctx, generated.AppendUsageDisputeParams{ID: uuid.NewString(), TenantID: scope.TenantID(), GrantID: r.ID, BillingSnapshotID: r.BillingSnapshotID, EvidenceSha256: digest[:], Reason: reason, Evidence: raw})
}

func (s MeteringService) IngestUsageBatch(ctx context.Context, scope TenantScope, in UsageInput, request string) (UsageAck, error) {
	var ack UsageAck
	digest := sha256.Sum256(in.Payload)
	if in.Sequence < 1 || in.BatchID == "" || len(in.Payload) == 0 || len(in.Payload) > 1<<20 || !bytes.Equal(digest[:], in.Digest) || len(in.Facts) == 0 || len(in.Facts) > 1024 {
		return ack, ErrConflict
	}
	if _, e := uuid.Parse(in.NodeID); e != nil {
		return ack, e
	}
	if _, e := uuid.Parse(in.BootID); e != nil {
		return ack, e
	}
	conflicted := false
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.ingest", ResourceID: in.NodeID, RequestID: request, Reason: "immutable boot/sequence/digest ingestion; replay never double charges"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, in.NodeID+"/"+in.BootID); e != nil {
			return e
		}
		old, e := q.GetUsageTuple(ctx, generated.GetUsageTupleParams{NodeID: in.NodeID, BootID: in.BootID, Sequence: in.Sequence})
		if errors.Is(e, pgx.ErrNoRows) {
			old, e = q.GetUsageBatchID(ctx, generated.GetUsageBatchIDParams{NodeID: in.NodeID, BootID: in.BootID, BatchID: in.BatchID})
		}
		if e == nil {
			if old.TenantID != scope.TenantID() {
				return ErrConflict
			}
			if old.BillingSnapshotID == in.SnapshotID && bytes.Equal(old.PayloadSha256, in.Digest) && old.BatchID == in.BatchID {
				return json.Unmarshal(old.Acknowledgement, &ack)
			}
			// Preserve the original grant/source even if the conflicting body names a
			// different snapshot or grant. Original facts are separately retained.
			var original []UsageFact
			if e = json.Unmarshal(old.Facts, &original); e != nil {
				return e
			}
			for _, f := range original {
				r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: f.GrantID})
				if e != nil {
					return e
				}
				if e = dispute(ctx, q, scope, r, "tuple digest/snapshot conflict", map[string]any{"node_id": in.NodeID, "boot_id": in.BootID, "sequence": in.Sequence, "digest": in.Digest}); e != nil {
					return e
				}
			}
			conflicted = true
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		facts := append([]UsageFact(nil), in.Facts...)
		sort.Slice(facts, func(i, j int) bool { return facts[i].GrantID < facts[j].GrantID })
		// Validate the entire batch before changing any source. A disputed member
		// cannot partially charge the other members of this batch.
		grants := make(map[string]generated.Reservation, len(facts))
		for i, f := range facts {
			if f.RawCumulative < 0 || (i > 0 && facts[i-1].GrantID == f.GrantID) {
				return ErrConflict
			}
			r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: f.GrantID})
			if e != nil {
				return e
			}
			grants[f.GrantID] = r
			if r.NodeID != in.NodeID || r.BootID != in.BootID || r.BillingSnapshotID != in.SnapshotID {
				if e = dispute(ctx, q, scope, r, "grant binding mismatch", in); e != nil {
					return e
				}
				conflicted = true
			}
		}
		if conflicted {
			return nil
		}
		sort.Slice(facts, func(i, j int) bool {
			a, b := grants[facts[i].GrantID], grants[facts[j].GrantID]
			if a.SourceID != b.SourceID {
				return a.SourceID < b.SourceID
			}
			return a.ID < b.ID
		})
		for _, f := range facts {
			r := grants[f.GrantID]
			if _, e = q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: r.SourceID}); e != nil {
				return e
			}
		}
		var raw int64
		bill := big.NewInt(0)
		for i, f := range facts {
			if f.RawCumulative < 0 || (i > 0 && facts[i-1].GrantID == f.GrantID) {
				return ErrConflict
			}
			r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: f.GrantID})
			if e != nil {
				return e
			}
			if r.NodeID != in.NodeID || r.BootID != in.BootID || r.BillingSnapshotID != in.SnapshotID {
				if e = dispute(ctx, q, scope, r, "grant binding mismatch", in); e != nil {
					return e
				}
				conflicted = true
				continue
			}
			if f.Role == "relay" || f.Role == "exit" {
				continue
			}
			if f.Role != "execution" {
				return ErrConflict
			}
			if in.Delta {
				a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: r.ID})
				if e != nil {
					return e
				}
				if math.MaxInt64-a.RawCumulative < f.RawCumulative {
					return ErrConflict
				}
				f.RawCumulative += a.RawCumulative
			}
			if math.MaxInt64-raw < f.RawCumulative {
				return ErrConflict
			}
			raw += f.RawCumulative
			bill.Add(bill, new(big.Int).Mul(big.NewInt(f.RawCumulative), big.NewInt(r.CoefficientMilli)))
			if e = settleActual(ctx, q, scope, r, "actual", fmt.Sprintf("batch/%s/%d", in.BootID, in.Sequence), f.RawCumulative, 0, in.Digest); e != nil {
				return e
			}
		}
		if conflicted {
			return nil
		}
		ack = UsageAck{BatchID: in.BatchID, Digest: in.Digest, State: "accepted"}
		return q.AppendUsageBatch(ctx, generated.AppendUsageBatchParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID, Sequence: in.Sequence, BatchID: in.BatchID, BillingSnapshotID: in.SnapshotID, PayloadSha256: in.Digest, Payload: in.Payload, RawCumulative: numerator(raw, 1), BillableNumerator: pgtype.Numeric{Int: bill, Valid: true}, Acknowledgement: auditJSON(ack), Facts: auditJSON(in.Facts)})
	})
	if e == nil && conflicted {
		e = ErrUsageDisputed
	}
	return ack, e
}

// settleActual runs only inside the shared AuditWriter transaction. All deltas
// are in the original source, snapshot and fixed-point coefficient.
func settleActual(ctx context.Context, q *generated.Queries, scope TenantScope, r generated.Reservation, kind, key string, raw, returned int64, proof any) error {
	p, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: r.SourceID})
	if e != nil {
		return e
	}
	old, e := q.GetSettlementReceipt(ctx, generated.GetSettlementReceiptParams{TenantID: scope.TenantID(), GrantID: r.ID, ReceiptKey: key})
	if e == nil {
		var a, b any
		_ = json.Unmarshal(old.Evidence, &a)
		_ = json.Unmarshal(auditJSON(proof), &b)
		if old.Kind != kind || old.RawCumulative != raw || old.ReturnedRaw != returned || !jsonEqual(a, b) {
			return ErrUsageDisputed
		}
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: r.ID})
	if e != nil {
		return e
	}
	if a.Terminal || raw < a.RawCumulative || raw > r.RawGranted-a.ReturnedRaw || returned < 0 || returned > r.RawGranted-a.ReturnedRaw-raw {
		return ErrConflict
	}
	delta := new(big.Int).Mul(big.NewInt(raw-a.RawCumulative), big.NewInt(r.CoefficientMilli))
	delta.Add(delta, big.NewInt(p.RemainderMilli))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(delta, big.NewInt(1000), remainder)
	if !quotient.IsInt64() {
		return ErrConflict
	}
	charge := quotient.Int64()
	release := int64(0)
	if kind == "final" {
		release = r.ReservedBill - a.Charged - a.ReleasedBill - charge
	}
	if kind == "return" {
		held, e := amount(r.RawGranted-a.ReturnedRaw-returned, r.CoefficientMilli, true)
		if e != nil {
			return e
		}
		release = r.ReservedBill - a.ReleasedBill - held
	}
	if release < 0 || charge+release > r.ReservedBill-a.Charged-a.ReleasedBill {
		return ErrConflict
	}
	debitB := min(p.B, charge)
	debitL := charge - debitB
	releaseL := min(p.L-debitL, release)
	if e = q.AppendUsageSettlement(ctx, generated.AppendUsageSettlementParams{TenantID: scope.TenantID(), GrantID: r.ID, BillingSnapshotID: r.BillingSnapshotID, Kind: kind, ReceiptKey: key, RawCumulative: raw, ReturnedRaw: returned, BillableNumerator: numerator(raw, r.CoefficientMilli), Evidence: auditJSON(proof), ChargedDelta: charge, ReleasedBill: release}); e != nil {
		return e
	}
	return ledgerEvent(ctx, q, scope, r.SourceID, r.BillingSnapshotID, r.ID+"/"+key, kind, -debitB, -charge-release, -debitL-releaseL, charge, remainder.Int64()-p.RemainderMilli, map[string]any{"grant_id": r.ID, "raw_cumulative": raw, "coefficient_milli": r.CoefficientMilli, "proof": proof})
}

func (s MeteringService) mutateReceipt(ctx context.Context, scope TenantScope, grant, snapshot, kind, key string, raw, returned int64, proof []byte, request string) error {
	if key == "" || len(proof) != 32 {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering." + kind, ResourceID: grant, RequestID: request, Reason: "immutable source-local authenticated receipt"}, func(q *generated.Queries) error {
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: grant})
		if e != nil {
			return e
		}
		if r.BillingSnapshotID != snapshot {
			return ErrUsageDisputed
		}
		return settleActual(ctx, q, scope, r, kind, key, raw, returned, proof)
	})
}
func (s MeteringService) CommitActualUsage(ctx context.Context, scope TenantScope, grant, snapshot, key string, raw int64, proof []byte, request string) error {
	return s.mutateReceipt(ctx, scope, grant, snapshot, "actual", key, raw, 0, proof, request)
}
func (s MeteringService) ReturnUnused(ctx context.Context, scope TenantScope, grant, snapshot, key string, raw, returned int64, proof []byte, request string) error {
	return s.mutateReceipt(ctx, scope, grant, snapshot, "return", key, raw, returned, proof, request)
}
func (s MeteringService) RecordFinalReceipt(ctx context.Context, scope TenantScope, grant, snapshot, key string, raw int64, proof []byte, request string) error {
	return s.mutateReceipt(ctx, scope, grant, snapshot, "final", key, raw, 0, proof, request)
}
func (s MeteringService) RecordDispute(ctx context.Context, scope TenantScope, grant, reason string, proof any, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.dispute", ResourceID: grant, RequestID: request, Reason: reason}, func(q *generated.Queries) error {
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: grant})
		if e != nil {
			return e
		}
		return dispute(ctx, q, scope, r, reason, proof)
	})
}
