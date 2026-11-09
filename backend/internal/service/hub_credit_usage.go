package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
	"vortex/backend/db/generated"
)

// usageReservation resolves immutable child IDs to their one original source
// reservation, including while replaying conflicting original batch evidence.
func usageReservation(ctx context.Context, q *generated.Queries, tenant, grant string) (generated.Reservation, error) {
	r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: tenant, ID: grant})
	if !errors.Is(e, pgx.ErrNoRows) {
		return r, e
	}
	c, e := q.HubCreditGrant(ctx, generated.HubCreditGrantParams{TenantID: tenant, GrantID: grant})
	if e != nil {
		return r, e
	}
	return q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: tenant, ID: c.ReservationID})
}

type childUsageUpdate struct {
	grant string
	raw   int64
}

// normalizeHubUsage is read-only. Only a fully validated and settled batch may
// publish the returned child projections. Financial settlement stays on parent
// aggregate bytes so fixed-point remainder is neither reset nor rounded twice.
func normalizeHubUsage(ctx context.Context, q *generated.Queries, scope TenantScope, in UsageInput) ([]UsageFact, []childUsageUpdate, error) {
	sums := map[string]int64{}
	parents := map[string]generated.Reservation{}
	roles := map[string]string{}
	seen := map[string]bool{}
	var passthrough []UsageFact
	var updates []childUsageUpdate
	for _, f := range in.Facts {
		if f.RawCumulative < 0 || seen[f.GrantID] && f.GrantID != "" {
			return nil, nil, ErrConflict
		}
		seen[f.GrantID] = true
		if f.GrantID == "" {
			passthrough = append(passthrough, f)
			continue
		}
		c, e := q.HubCreditGrant(ctx, generated.HubCreditGrantParams{TenantID: scope.TenantID(), GrantID: f.GrantID})
		if errors.Is(e, pgx.ErrNoRows) {
			r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: f.GrantID})
			if e != nil {
				return nil, nil, e
			}
			allocated, e := q.ReservationHasHubAllocation(ctx, generated.ReservationHasHubAllocationParams{TenantID: scope.TenantID(), ReservationID: r.ID})
			if e != nil {
				return nil, nil, e
			}
			if allocated {
				return nil, nil, ErrConflict
			}
			n := f.RawCumulative
			if in.Delta && f.Role == "execution" {
				a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: r.ID})
				if e != nil {
					return nil, nil, e
				}
				if n > math.MaxInt64-a.RawCumulative {
					return nil, nil, ErrConflict
				}
				n += a.RawCumulative
			}
			passthrough = append(passthrough, UsageFact{GrantID: f.GrantID, RawCumulative: n, Role: f.Role})
			continue
		}
		if e != nil {
			return nil, nil, e
		}
		if c.NodeID != in.NodeID || c.BootID != in.BootID || f.Role != "execution" {
			return nil, nil, ErrUsageDisputed
		}
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: c.ReservationID})
		if e != nil {
			return nil, nil, e
		}
		if r.BillingSnapshotID != in.SnapshotID {
			return nil, nil, ErrUsageDisputed
		}
		previous, e := q.HubCreditUsage(ctx, generated.HubCreditUsageParams{TenantID: scope.TenantID(), GrantID: f.GrantID})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, nil, e
		}
		n := f.RawCumulative
		if in.Delta {
			if n > math.MaxInt64-previous {
				return nil, nil, ErrConflict
			}
			n += previous
		}
		if n < previous || n > c.RawGranted {
			return nil, nil, ErrUsageDisputed
		}
		delta := n - previous
		if delta > math.MaxInt64-sums[r.ID] {
			return nil, nil, ErrConflict
		}
		sums[r.ID] += delta
		parents[r.ID] = r
		roles[r.ID] = f.Role
		updates = append(updates, childUsageUpdate{f.GrantID, n})
	}
	for id, n := range sums {
		a, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: scope.TenantID(), GrantID: id})
		if e != nil {
			return nil, nil, e
		}
		if n > parents[id].RawGranted-a.RawCumulative {
			return nil, nil, ErrUsageDisputed
		}
		passthrough = append(passthrough, UsageFact{GrantID: id, RawCumulative: a.RawCumulative + n, Role: roles[id]})
	}
	return passthrough, updates, nil
}
