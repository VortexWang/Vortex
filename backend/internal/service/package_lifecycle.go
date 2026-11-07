package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// ProcessPackageLifecycle is a worker-only clock boundary. It issues the current
// period once (never missed/future periods), or records expiry. Source expiry is
// an append-only fact, never a balance transfer or release of unknown liability.
func (s PackageService) ProcessPackageLifecycle(ctx context.Context, at time.Time) (bool, error) {
	at = at.UTC().Truncate(time.Microsecond)
	rows, e := generated.New(s.Pool).DuePackageLifecycle(ctx, timestamp(at))
	if e != nil || len(rows) == 0 {
		return false, e
	}
	tenant := rows[0]
	actor := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/package-lifecycle/"+tenant)).String()
	scope, e := NewTenantScope(actor, tenant, tenant)
	if e != nil {
		return false, e
	}
	event := repository.Audit{Action: "package.lifecycle", ResourceID: tenant, RequestID: uuid.NewString(), Reason: "trusted clock: immutable current period or package expiry; no debt release"}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &event, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, tenant); e != nil {
			return e
		}
		sub, e := q.LockSubscription(ctx, tenant)
		if e != nil {
			return e
		}
		snap, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: tenant, ID: sub.SnapshotID})
		if e != nil {
			return e
		}
		var terms PackageTerms
		if e = json.Unmarshal(snap.Terms, &terms); e != nil {
			return e
		}
		expired := !at.Before(sub.ExpiresAt.Time)
		boundary := sub.ExpiresAt.Time
		kind := "expiry/" + strconv.FormatInt(sub.Version, 10)
		if !expired {
			boundary = sub.AnchorAt.Time
			if terms.TrafficReset == "monthly" {
				boundary, _ = MonthlyPeriod(sub.AnchorAt.Time, at)
			}
			kind = "period/" + boundary.Format("20060102T150405.000000Z")
		}
		event.RequestID = "package-lifecycle/" + sub.ID + "/" + kind
		event.After = auditJSON(struct {
			Snapshot    string    `json:"snapshot_id"`
			Expired     bool      `json:"expired"`
			EffectiveAt time.Time `json:"effective_at"`
		}{snap.ID, expired, boundary})
		// Existing source identities remain unchanged. Append expiration only
		// after the effective deadline, with extensions taken into account.
		sources, e := q.ListTrafficSources(ctx, tenant)
		if e != nil {
			return e
		}
		for _, source := range sources {
			if source.Kind != "base" {
				continue
			}
			until, e := q.SourceEffectiveExpiry(ctx, generated.SourceEffectiveExpiryParams{TenantID: tenant, ID: source.ID})
			if e != nil {
				return e
			}
			if !at.Before(until.Time) {
				reason := "period_end"
				if expired {
					reason = "expiry"
				}
				if e = q.ExpireSource(ctx, generated.ExpireSourceParams{TenantID: tenant, SourceID: source.ID, EffectiveAt: until, Reason: reason}); e != nil {
					return e
				}
			}
		}
		current, e := q.GetCurrentRestriction(ctx, generated.GetCurrentRestrictionParams{TenantID: tenant, Cause: "package_expired", SourceID: sub.ID})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if expired {
			if e == nil && current.SourceVersion == sub.Version && !current.ClearedAt.Valid {
				return nil
			}
			id := uuid.NewString()
			if _, e = q.AppendRestrictionEvent(ctx, generated.AppendRestrictionEventParams{ID: id, TenantID: tenant, Cause: "package_expired", SourceID: sub.ID, SourceVersion: sub.Version, EventKind: "applied", EffectiveAt: sub.ExpiresAt}); e != nil {
				return e
			}
			if _, e = q.ApplyCurrentRestriction(ctx, generated.ApplyCurrentRestrictionParams{TenantID: tenant, Cause: "package_expired", SourceID: sub.ID, SourceVersion: sub.Version, EventID: id, EffectiveAt: sub.ExpiresAt}); e != nil {
				return e
			}
			ports, e := q.TenantPackagePorts(ctx, tenant)
			if e != nil {
				return e
			}
			for _, port := range ports {
				if port.State == "releasing" || port.State == "aborting" {
					continue
				}
				if _, e = lockAllocation(ctx, q, scope, port.ID); e != nil {
					return e
				}
				if e = q.RetainPortAllocation(ctx, generated.RetainPortAllocationParams{TenantID: tenant, ID: port.ID, EffectiveAt: sub.ExpiresAt}); e != nil {
					return e
				}
				if e = q.RetainPackageReservations(ctx, generated.RetainPackageReservationsParams{TenantID: tenant, AllocationID: port.ID}); e != nil {
					return e
				}
				if e = domainTask(ctx, q, scope, port.RuleID, "rule.stop_restricted", "resource_change", sub.Version); e != nil {
					return e
				}
			}
			return domainTask(ctx, q, scope, sub.ID, "package.expired", "resource_change", sub.Version)
		}
		if e == nil && !current.ClearedAt.Valid && !at.Before(current.EffectiveAt.Time) {
			id := uuid.NewString()
			if _, e = q.AppendRestrictionEvent(ctx, generated.AppendRestrictionEventParams{ID: id, TenantID: tenant, Cause: "package_expired", SourceID: sub.ID, SourceVersion: current.SourceVersion, EventKind: "cleared", EffectiveAt: current.EffectiveAt, ClearedAt: timestamp(at)}); e != nil {
				return e
			}
			if _, e = q.ClearCurrentRestriction(ctx, generated.ClearCurrentRestrictionParams{TenantID: tenant, Cause: "package_expired", SourceID: sub.ID, SourceVersion: current.SourceVersion, EventID: id, ClearedAt: timestamp(at)}); e != nil {
				return e
			}
			if e = clearTenantPortClocks(ctx, q, scope); e != nil {
				return e
			}
		}
		_, e = issueBase(ctx, q, scope, sub, snap, terms, at)
		return e
	})
	return true, e
}
