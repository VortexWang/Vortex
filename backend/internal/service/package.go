package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type PackageService struct{ Pool *pgxpool.Pool }
type PackageTerms struct {
	PriceMinor      int64    `json:"price_minor"`
	DurationDays    int32    `json:"duration_days"`
	TrafficReset    string   `json:"traffic_reset"`
	TrafficBytes    int64    `json:"traffic_bytes"`
	Capabilities    []string `json:"capabilities"`
	RuleLimit       int64    `json:"rule_limit"`
	RateLimit       int64    `json:"rate_limit"`
	ConnectionLimit int64    `json:"connection_limit"`
}
type PackageResult struct {
	SubscriptionID string    `json:"subscription_id"`
	SnapshotID     string    `json:"snapshot_id"`
	SourceID       string    `json:"source_id"`
	OperationID    string    `json:"operation_id"`
	Version        int64     `json:"version"`
	AnchorAt       time.Time `json:"anchor_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func normalizeTerms(p PackageTerms) (PackageTerms, error) {
	if p.DurationDays < 1 || p.DurationDays > 36500 || p.PriceMinor < 0 || p.TrafficBytes < 0 || p.RuleLimit < 0 || p.RateLimit < 0 || p.ConnectionLimit < 0 {
		return p, ErrConflict
	}
	if p.TrafficReset == "" {
		p.TrafficReset = "monthly"
		if p.DurationDays <= 31 {
			p.TrafficReset = "none"
		}
	}
	if p.TrafficReset != "none" && p.TrafficReset != "monthly" {
		return p, ErrConflict
	}
	p.Capabilities = append([]string{}, p.Capabilities...)
	sort.Strings(p.Capabilities)
	for i, c := range p.Capabilities {
		if c != "tcp" && c != "udp" && c != "direct" && c != "tunnel" && c != "multi_hop" && c != "manual_reverse" {
			return p, ErrConflict
		}
		if i > 0 && c == p.Capabilities[i-1] {
			return p, ErrConflict
		}
	}
	return p, nil
}

// EffectiveEntitlements intersects every independent authorization. Zero is a
// real zero limit here; no source can widen another source's authority.
func EffectiveEntitlements(all ...PackageTerms) PackageTerms {
	if len(all) == 0 {
		return PackageTerms{Capabilities: []string{}}
	}
	out := all[0]
	out.Capabilities = append([]string{}, out.Capabilities...)
	for _, p := range all[1:] {
		caps := []string{}
		for _, c := range out.Capabilities {
			if hasCapability(p, c) {
				caps = append(caps, c)
			}
		}
		out.Capabilities = caps
		out.RuleLimit = min(out.RuleLimit, p.RuleLimit)
		out.RateLimit = min(out.RateLimit, p.RateLimit)
		out.ConnectionLimit = min(out.ConnectionLimit, p.ConnectionLimit)
	}
	sort.Strings(out.Capabilities)
	return out
}
func hasCapability(p PackageTerms, c string) bool {
	for _, v := range p.Capabilities {
		if v == c {
			return true
		}
	}
	return false
}

// MonthlyPeriod always derives from the original UTC day/time, never from a
// previously clamped February date. Intervals are half open.
func MonthlyPeriod(anchor, at time.Time) (time.Time, time.Time) {
	anchor = anchor.UTC()
	at = at.UTC()
	nth := func(n int) time.Time {
		first := time.Date(anchor.Year(), anchor.Month()+time.Month(n), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
		last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		return time.Date(first.Year(), first.Month(), min(anchor.Day(), last), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
	}
	n := (at.Year()-anchor.Year())*12 + int(at.Month()-anchor.Month())
	if n < 0 {
		n = 0
	}
	if n > 0 && at.Before(nth(n)) {
		n--
	}
	return nth(n), nth(n + 1)
}
func (s PackageService) PutCatalog(ctx context.Context, scope TenantScope, id, name string, expected int64, terms PackageTerms, request string) (generated.PackageVersion, error) {
	var result generated.PackageVersion
	if !scope.Platform() || !scope.Allows(policy.Mutate) {
		return result, ErrScope
	}
	terms, e := normalizeTerms(terms)
	if e != nil || !validID(id) || len(name) == 0 || len(name) > 128 || expected < 0 {
		return result, ErrConflict
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "package.catalog", ResourceID: id, RequestID: request, Reason: "append future-only catalog version", After: auditJSON(terms)}, func(q *generated.Queries) error {
		if expected == 0 {
			if e := q.CreatePackageCatalog(ctx, generated.CreatePackageCatalogParams{ID: id, Name: name}); e != nil {
				return e
			}
		} else {
			old, e := q.LockPackageCatalog(ctx, id)
			if e != nil {
				return e
			}
			if old.CurrentVersion != expected {
				return ErrConflict
			}
			if e = q.AdvancePackageCatalog(ctx, generated.AdvancePackageCatalogParams{ID: id, Name: name, Enabled: true}); e != nil {
				return e
			}
		}
		var e error
		result, e = q.AppendPackageVersion(ctx, generated.AppendPackageVersionParams{PackageID: id, Version: expected + 1, Terms: auditJSON(terms)})
		if e != nil {
			return e
		}
		return domainTask(ctx, q, scope, id, "package.catalog.changed", "resource_change", expected+1)
	})
	return result, e
}
func packageSnapshot(ctx context.Context, q *generated.Queries, tenant, pkg string, version int64) (generated.PackageSnapshot, PackageTerms, error) {
	var p PackageTerms
	v, e := q.GetPackageVersion(ctx, generated.GetPackageVersionParams{PackageID: pkg, Version: version})
	if e != nil {
		return generated.PackageSnapshot{}, p, e
	}
	if e = json.Unmarshal(v.Terms, &p); e != nil {
		return generated.PackageSnapshot{}, p, e
	}
	p, e = normalizeTerms(p)
	if e != nil {
		return generated.PackageSnapshot{}, p, e
	}
	snap, e := q.AppendPackageSnapshot(ctx, generated.AppendPackageSnapshotParams{ID: uuid.NewString(), TenantID: tenant, PackageID: pkg, PackageVersion: version, Terms: v.Terms})
	return snap, p, e
}
func requestReplay(ctx context.Context, q *generated.Queries, tenant, key string, payload any, out any) (bool, error) {
	if len(key) < 1 || len(key) > 128 {
		return false, ErrConflict
	}
	if e := q.LockPackageRequestKey(ctx, key); e != nil {
		return false, e
	}
	r, e := q.GetPackageRequest(ctx, key)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	d := sha256.Sum256(auditJSON(payload))
	if r.TenantID != tenant || !bytes.Equal(r.PayloadSha256, d[:]) {
		return false, ErrConflict
	}
	if e = json.Unmarshal(r.Result, out); e != nil {
		return false, e
	}
	return true, nil
}
func savePackageRequest(ctx context.Context, q *generated.Queries, tenant, key string, payload, result any) error {
	d := sha256.Sum256(auditJSON(payload))
	return q.AppendPackageRequest(ctx, generated.AppendPackageRequestParams{RequestKey: key, TenantID: tenant, PayloadSha256: d[:], Result: auditJSON(result)})
}

// Subscribe is a local administrative issuance operation, never a payment or
// self-service purchase. User issuance uses the separately locked redeem path.
func (s PackageService) Subscribe(ctx context.Context, scope TenantScope, pkg string, version int64, key, request string) (PackageResult, error) {
	var result PackageResult
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return result, ErrScope
	}
	payload := struct {
		Kind, Package string
		Version       int64
	}{"subscribe", pkg, version}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "package.subscribe", ResourceID: pkg, RequestID: request, Reason: "explicit administrative package issuance"}, func(q *generated.Queries) error {
		replay, e := requestReplay(ctx, q, scope.TenantID(), key, payload, &result)
		if e != nil || replay {
			return e
		}
		if _, e = q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		if _, e = q.LockSubscription(ctx, scope.TenantID()); !errors.Is(e, pgx.ErrNoRows) {
			if e == nil {
				return ErrConflict
			}
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		snap, p, e := packageSnapshot(ctx, q, scope.TenantID(), pkg, version)
		if e != nil {
			return e
		}
		sub, e := q.CreateSubscription(ctx, generated.CreateSubscriptionParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SnapshotID: snap.ID, AnchorAt: timestamp(now), ExpiresAt: timestamp(now.AddDate(0, 0, int(p.DurationDays)))})
		if e != nil {
			return e
		}
		result, e = issueBase(ctx, q, scope, sub, snap, p, now)
		if e != nil {
			return e
		}
		return savePackageRequest(ctx, q, scope.TenantID(), key, payload, result)
	})
	return result, e
}
func issueBase(ctx context.Context, q *generated.Queries, scope TenantScope, sub generated.Subscription, snap generated.PackageSnapshot, p PackageTerms, at time.Time) (PackageResult, error) {
	from, until := sub.AnchorAt.Time, sub.ExpiresAt.Time
	if p.TrafficReset == "monthly" {
		from, until = MonthlyPeriod(sub.AnchorAt.Time, at)
		until = minTime(until, sub.ExpiresAt.Time)
	}
	period := snap.ID + "/" + from.Format(time.RFC3339Nano)
	existing, e := q.ListTrafficSources(ctx, scope.TenantID())
	if e != nil {
		return PackageResult{}, e
	}
	for _, v := range existing {
		if v.Kind == "base" && v.PeriodKey == period {
			return packageResult(sub, v.ID, ""), nil
		}
	}
	source, e := q.AppendTrafficSource(ctx, generated.AppendTrafficSourceParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SnapshotID: snap.ID, SubscriptionID: pgtype.Text{String: sub.ID, Valid: true}, Kind: "base", InitialAmount: p.TrafficBytes, IssuedAt: timestamp(from), ExpiresAt: timestamp(until), PeriodKey: period})
	if e != nil {
		return PackageResult{}, e
	}
	op, e := newDomainTask(ctx, q, scope, sub.ID, "package.source.issued", "resource_change", sub.Version)
	if e != nil {
		return PackageResult{}, e
	}
	return packageResult(sub, source.ID, op.ID), nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func packageResult(s generated.Subscription, source, op string) PackageResult {
	return PackageResult{SubscriptionID: s.ID, SnapshotID: s.SnapshotID, SourceID: source, OperationID: op, Version: s.Version, AnchorAt: s.AnchorAt.Time, ExpiresAt: s.ExpiresAt.Time}
}
func (s PackageService) Subscription(ctx context.Context, scope TenantScope, request string) (generated.Subscription, PackageTerms, error) {
	var sub generated.Subscription
	var p PackageTerms
	if !scope.Allows(policy.Read) {
		return sub, p, ErrScope
	}
	read := func(q *generated.Queries) error {
		var e error
		sub, e = q.GetSubscription(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		snap, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: sub.SnapshotID})
		if e != nil {
			return e
		}
		return json.Unmarshal(snap.Terms, &p)
	}
	var e error
	if scope.Platform() {
		e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "package.read", ResourceID: scope.TenantID(), RequestID: request, Reason: "platform subscription read"}, read)
	} else {
		e = read(generated.New(s.Pool))
	}
	if e != nil {
		return generated.Subscription{}, PackageTerms{}, e
	}
	return sub, p, nil
}

// ConsumableSources orders isolated issuance records; balances/commitments are
// supplied by 00007. Expiry never deletes a source or releases unknown debt.
func (s PackageService) ConsumableSources(ctx context.Context, scope TenantScope, at time.Time) ([]generated.TrafficSource, error) {
	if !scope.Allows(policy.Read) || scope.Platform() {
		return nil, ErrScope
	}
	q := generated.New(s.Pool)
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if e != nil {
		return nil, e
	}
	if !at.Before(sub.ExpiresAt.Time) {
		return []generated.TrafficSource{}, nil
	}
	tenant, e := q.GetTenant(ctx, scope.TenantID())
	if e != nil {
		return nil, e
	}
	restrictions, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(at)})
	if e != nil {
		return nil, e
	}
	if tenant.AccountState != "active" || len(restrictions) > 0 {
		return []generated.TrafficSource{}, nil
	}
	rows, e := q.ListTrafficSources(ctx, scope.TenantID())
	if e != nil {
		return nil, e
	}
	out := []generated.TrafficSource{}
	for _, r := range rows {
		expired, e := q.SourceIsExpired(ctx, generated.SourceIsExpiredParams{TenantID: scope.TenantID(), SourceID: r.ID, AtTime: timestamp(at)})
		if e != nil {
			return nil, e
		}
		if !expired && !at.Before(r.IssuedAt.Time) && at.Before(r.ExpiresAt.Time) {
			out = append(out, r)
		}
	}
	return out, nil
}
