package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type LedgerService struct{ Pool *pgxpool.Pool }

func ledgerEvent(ctx context.Context, q *generated.Queries, scope TenantScope, source, snapshot, key, kind string, b, f, l, charged, remainder int64, evidence any) error {
	_, e := q.AppendLedgerEvent(ctx, generated.AppendLedgerEventParams{TenantID: scope.TenantID(), SourceID: source, BillingSnapshotID: pgtype.Text{String: snapshot, Valid: snapshot != ""}, EventKey: key, Kind: kind, DeltaB: b, DeltaF: f, DeltaL: l, DeltaCharged: charged, DeltaRemainder: remainder, Evidence: auditJSON(evidence)})
	return e
}

// InitializeSource consumes the actual immutable package issuance. No initial
// credit supplied by a caller, cache, grant request or new boot can create funds.
func (s LedgerService) InitializeSource(ctx context.Context, scope TenantScope, source, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "ledger.initialize", ResourceID: source, RequestID: request, Reason: "rebuildable source authority from immutable package issuance"}, func(q *generated.Queries) error {
		row, e := q.MeteringSource(ctx, generated.MeteringSourceParams{TenantID: scope.TenantID(), ID: source, AtTime: timestamp(time.Now())})
		if e != nil {
			return e
		}
		if e = q.EnsureSourceLedger(ctx, generated.EnsureSourceLedgerParams{TenantID: scope.TenantID(), ID: source}); e != nil {
			return e
		}
		if e = q.EnsureLedgerProjection(ctx, generated.EnsureLedgerProjectionParams{TenantID: scope.TenantID(), SourceID: source}); e != nil {
			return e
		}
		p, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source})
		if e != nil {
			return e
		}
		if p.Sequence != 0 {
			return nil
		}
		return ledgerEvent(ctx, q, scope, source, "", "origin", "origin", row.InitialAmount, 0, 0, 0, 0, map[string]any{"package_snapshot_id": row.SnapshotID, "initial_amount": row.InitialAmount})
	})
}

// ExpireSource preserves every outstanding commitment as old-source L. This
// does not freeze another source or transfer the old debt to a new package.
func (s LedgerService) ExpireSource(ctx context.Context, scope TenantScope, source, request string, at time.Time) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "ledger.expiry", ResourceID: source, RequestID: request, Reason: "retain old source commitments without cross-source transfer"}, func(q *generated.Queries) error {
		row, e := q.MeteringSource(ctx, generated.MeteringSourceParams{TenantID: scope.TenantID(), ID: source, AtTime: timestamp(at)})
		if e != nil {
			return e
		}
		if !row.Expired && at.Before(row.EffectiveUntil.Time) {
			return ErrConflict
		}
		p, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source})
		if e != nil {
			return e
		}
		if _, e = q.GetLedgerEvent(ctx, generated.GetLedgerEventParams{TenantID: scope.TenantID(), SourceID: source, EventKey: "expiry"}); e == nil {
			return nil
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		return ledgerEvent(ctx, q, scope, source, "", "expiry", "expiry", -p.B, 0, p.F-p.L, 0, 0, map[string]any{"effective_at": at.UTC()})
	})
}

func (s LedgerService) Rebuild(ctx context.Context, scope TenantScope, source, request string) (generated.LedgerProjection, error) {
	var out generated.LedgerProjection
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "ledger.rebuild", ResourceID: source, RequestID: request, Reason: "verify ordered immutable hash chain and restore projection"}, func(q *generated.Queries) error {
		if _, e := q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source}); e != nil {
			return e
		}
		row, e := q.RebuildLedgerProjection(ctx, generated.RebuildLedgerProjectionParams{T: scope.TenantID(), S: source})
		if e != nil {
			return e
		}
		if !row.Verified || row.F-row.L > row.B {
			return ErrConflict
		}
		last, e := q.LastSourceEvent(ctx, generated.LastSourceEventParams{TenantID: scope.TenantID(), SourceID: source})
		if e != nil {
			return e
		}
		if e = q.RestoreSourceProjection(ctx, generated.RestoreSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source, B: row.B, F: row.F, L: row.L, Charged: row.Charged, RemainderMilli: row.RemainderMilli, Sequence: row.Sequence, Digest: last.Digest}); e != nil {
			return e
		}
		out, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source})
		return e
	})
	return out, e
}

type BillingGroup struct {
	Group      string `json:"group_id"`
	Version    int64  `json:"version"`
	Multiplier int64  `json:"multiplier_milli"`
}
type BillingExplanation struct {
	SnapshotID       string         `json:"billing_snapshot_id"`
	CoefficientMilli int64          `json:"path_coefficient_milli"`
	Groups           []BillingGroup `json:"groups"`
	Formula          string         `json:"explanation"`
}

func (s LedgerService) BillingExplanation(ctx context.Context, scope TenantScope, id, request string) (BillingExplanation, error) {
	var out BillingExplanation
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "billing.explanation", ResourceID: id, RequestID: request, Reason: "immutable path and each group multiplier for billing preview"}, func(q *generated.Queries) error {
		b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: id})
		if e != nil {
			return e
		}
		out = BillingExplanation{SnapshotID: id, CoefficientMilli: b.MultiplierMilli, Formula: "计费流量 = 执行点双向载荷 × 路径系数；跨授权按来源累计千分位，用户扣减取累计整数部分"}
		return json.Unmarshal(b.PathVersions, &out.Groups)
	})
	return out, e
}

// ScheduleRateVersion records an explicit synchronized policy, never rewrites
// any snapshot. The future signer producer must supply its own sync proof;
// merely storing this candidate does not activate a lease or runtime authority.
func (s LedgerService) ScheduleRateVersion(ctx context.Context, scope TenantScope, oldID string, groups []BillingGroup, from, until, announced, synced time.Time, proof, request string) (string, error) {
	var id string
	if !scope.Platform() || proof == "" || !from.Before(until) || synced.Before(announced) || from.Before(synced.Add(24*time.Hour)) || synced.After(time.Now()) {
		return id, ErrConflict
	}
	var coefficient int64
	for _, g := range groups {
		if g.Group == "" || g.Version < 1 || g.Multiplier < 0 || g.Multiplier > 1_000_000_000 || coefficient > 1_000_000_000-g.Multiplier {
			return id, ErrConflict
		}
		coefficient += g.Multiplier
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "billing.rate_version", ResourceID: oldID, RequestID: request, Reason: "24 hour synchronized policy candidate; no automatic signer activation"}, func(q *generated.Queries) error {
		old, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: oldID})
		if e != nil {
			return e
		}
		var previous []BillingGroup
		if e = json.Unmarshal(old.PathVersions, &previous); e != nil {
			return e
		}
		if len(previous) != len(groups) {
			return ErrConflict
		}
		for i, g := range groups {
			if previous[i].Group != g.Group || g.Version < previous[i].Version {
				return ErrConflict
			}
		}
		row, e := q.ScheduleBillingSnapshot(ctx, generated.ScheduleBillingSnapshotParams{ID: uuid.NewString(), TenantID: scope.TenantID(), PackageSnapshotID: old.PackageSnapshotID, RuleID: old.RuleID, RuleRevision: old.RuleRevision, PathGeneration: old.PathGeneration, Version: old.Version + 1, MultiplierMilli: coefficient, PathVersions: auditJSON(groups), ValidFrom: timestamp(from), ValidUntil: timestamp(until), AnnouncedAt: timestamp(announced), SynchronizedAt: timestamp(synced)})
		if e != nil {
			return e
		}
		id = row.ID
		return q.AppendPolicyBoundary(ctx, generated.AppendPolicyBoundaryParams{TenantID: scope.TenantID(), OldSnapshotID: oldID, NewSnapshotID: id, BoundaryAt: timestamp(from), Evidence: auditJSON(map[string]any{"synchronization_proof": proof})})
	})
	return id, e
}

func (s LedgerService) RebuildCache(ctx context.Context, scope TenantScope, address string) ([]byte, error) {
	if !scope.Allows(policy.Read) || scope.Platform() {
		return nil, ErrScope
	}
	rows, e := generated.New(s.Pool).ListSourceProjections(ctx, scope.TenantID())
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(rows)
	if e != nil {
		return nil, e
	}
	_, e = CacheCommand(ctx, address, "SET", "vortex:source-balances:"+scope.TenantID(), string(raw))
	return raw, e
}
