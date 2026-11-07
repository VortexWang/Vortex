package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

var ErrPackageConfirmationRequired = errors.New("PACKAGE_CONFIRMATION_REQUIRED")
var ErrEntitlementInvalid = errors.New("ENTITLEMENT_NOT_APPLICABLE")

// Only a hash is stored; codes never enter audit/task/outbox summaries.
func (s PackageService) CreateEntitlement(ctx context.Context, scope TenantScope, code, kind, pkg string, version int64, days int32, amount, uses int64, until time.Time, tenant, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return ErrScope
	}
	if len(code) < 16 || len(code) > 256 || strings.TrimSpace(code) != code || !validID(pkg) || version < 1 || days < 1 || days > 36500 || amount < 0 || uses < 1 || !until.After(time.Now()) || kind != "package" && kind != "duration" && kind != "traffic" || tenant != "" && !validID(tenant) {
		return ErrConflict
	}
	digest := sha256.Sum256([]byte(code))
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "entitlement.create", ResourceID: pkg, RequestID: request, Reason: "administrative entitlement issuance", After: auditJSON(struct {
		Kind string
		Uses int64
	}{kind, uses})}, func(q *generated.Queries) error {
		if _, e := q.GetPackageVersion(ctx, generated.GetPackageVersionParams{PackageID: pkg, Version: version}); e != nil {
			return e
		}
		if e := q.CreateEntitlementCode(ctx, generated.CreateEntitlementCodeParams{CodeHash: digest[:], Kind: kind, PackageID: pkg, PackageVersion: version, DurationDays: days, Amount: amount, RemainingUses: uses, ValidUntil: timestamp(until), ApplicableTenant: pgtype.Text{String: tenant, Valid: tenant != ""}}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, pkg, "entitlement.created", "resource_change", 1)
	})
}
func (s PackageService) RevokeEntitlement(ctx context.Context, scope TenantScope, code, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return ErrScope
	}
	d := sha256.Sum256([]byte(code))
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "entitlement.revoke", ResourceID: scope.TenantID(), RequestID: request, Reason: "revoke unredeemed entitlement authority"}, func(q *generated.Queries) error {
		if _, e := q.LockEntitlementCode(ctx, d[:]); e != nil {
			return e
		}
		return q.RevokeEntitlementCode(ctx, d[:])
	})
}
func lockedCode(ctx context.Context, q *generated.Queries, tenant, code string) (generated.EntitlementCode, error) {
	d := sha256.Sum256([]byte(code))
	c, e := q.LockEntitlementCode(ctx, d[:])
	if errors.Is(e, pgx.ErrNoRows) {
		return c, ErrEntitlementInvalid
	}
	if e != nil {
		return c, e
	}
	if !codeApplicable(c, tenant) {
		return c, ErrEntitlementInvalid
	}
	return c, nil
}

func codeApplicable(c generated.EntitlementCode, tenant string) bool {
	return !c.Revoked && c.RemainingUses > 0 && time.Now().Before(c.ValidUntil.Time) && (!c.ApplicableTenant.Valid || c.ApplicableTenant.String == tenant)
}
func codeBinding(c generated.EntitlementCode, actor string) *entitlementBinding {
	return &entitlementBinding{CodeHash: hex.EncodeToString(c.CodeHash), ActorID: actor, DurationDays: c.DurationDays, Amount: c.Amount}
}

// PreviewEntitlement is read-only; the digest binds the exact code grant and
// actor to the same impact inputs used by ordinary package changes.
func (s PackageService) PreviewEntitlement(ctx context.Context, scope TenantScope, code string, keep []string, at time.Time) (json.RawMessage, error) {
	if !scope.Allows(policy.Mutate) || scope.Platform() {
		return nil, ErrScope
	}
	if len(code) < 16 || len(code) > 256 {
		return nil, ErrEntitlementInvalid
	}
	var preview downgradePreview
	e := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		q := generated.New(tx)
		hash := sha256.Sum256([]byte(code))
		c, e := q.GetEntitlementCode(ctx, hash[:])
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrEntitlementInvalid
		}
		if e != nil {
			return e
		}
		if c.Kind != "package" || !codeApplicable(c, scope.TenantID()) {
			return ErrEntitlementInvalid
		}
		preview, e = previewDowngrade(ctx, q, scope, c.PackageID, c.PackageVersion, keep, at)
		if e != nil {
			return e
		}
		preview = bindEntitlement(preview, codeBinding(c, scope.ActorID()))
		return nil
	})
	if e != nil {
		return nil, e
	}
	return json.Marshal(preview)
}

// Accepted future grants are consumed at confirmation. Failed due changes do
// not silently refund or reissue an already accepted entitlement.
func (s PackageService) ConfirmEntitlement(ctx context.Context, scope TenantScope, code string, keep []string, at time.Time, digest, key, request string) (PackageResult, error) {
	if len(code) < 16 || len(code) > 256 {
		return PackageResult{}, ErrEntitlementInvalid
	}
	return s.confirmPackageChange(ctx, scope, "", 0, code, keep, at, digest, key, request)
}

// Code duration and amount are immutable grant terms; catalog capability and
// reset policy remain pinned to the code's exact package version.
func entitlementSnapshot(ctx context.Context, q *generated.Queries, tenant string, c generated.EntitlementCode) (generated.PackageSnapshot, PackageTerms, error) {
	v, e := q.GetPackageVersion(ctx, generated.GetPackageVersionParams{PackageID: c.PackageID, Version: c.PackageVersion})
	var p PackageTerms
	if e != nil {
		return generated.PackageSnapshot{}, p, e
	}
	if e = json.Unmarshal(v.Terms, &p); e != nil {
		return generated.PackageSnapshot{}, p, e
	}
	p.DurationDays, p.TrafficBytes = c.DurationDays, c.Amount
	snap, e := q.AppendPackageSnapshot(ctx, generated.AppendPackageSnapshotParams{ID: uuid.NewString(), TenantID: tenant, PackageID: c.PackageID, PackageVersion: c.PackageVersion, Terms: auditJSON(p)})
	return snap, p, e
}
func (s PackageService) RedeemEntitlement(ctx context.Context, scope TenantScope, code, key, request string) (PackageResult, error) {
	var result PackageResult
	if !scope.Allows(policy.Mutate) {
		return result, ErrScope
	}
	if len(code) < 16 || len(code) > 256 {
		return result, ErrEntitlementInvalid
	}
	hash := sha256.Sum256([]byte(code))
	payload := struct {
		Kind string
		Code [32]byte
	}{"redeem", hash}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "entitlement.redeem", ResourceID: scope.TenantID(), RequestID: request, Reason: "atomic entitlement redemption; code excluded"}, func(q *generated.Queries) error {
		replay, e := requestReplay(ctx, q, scope.TenantID(), key, payload, &result)
		if e != nil || replay {
			return e
		}
		if _, e = q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		c, e := lockedCode(ctx, q, scope.TenantID(), code)
		if e != nil {
			return e
		}
		result, e = redeemCode(ctx, q, scope, c, key)
		if e != nil {
			return e
		}
		n, e := q.ConsumeEntitlementCode(ctx, hash[:])
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrEntitlementInvalid
		}
		if e = savePackageRequest(ctx, q, scope.TenantID(), key, payload, result); e != nil {
			return e
		}
		return q.AppendEntitlementRedemption(ctx, generated.AppendEntitlementRedemptionParams{RequestKey: key, TenantID: scope.TenantID(), CodeHash: hash[:], Result: auditJSON(result)})
	})
	if e != nil {
		return PackageResult{}, e
	}
	return result, nil
}
func redeemCode(ctx context.Context, q *generated.Queries, scope TenantScope, c generated.EntitlementCode, key string) (PackageResult, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	sub, e := q.LockSubscription(ctx, scope.TenantID())
	exists := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return PackageResult{}, e
	}
	var old generated.PackageSnapshot
	var p PackageTerms
	if exists {
		old, e = q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: sub.SnapshotID})
		if e != nil {
			return PackageResult{}, e
		}
		if e = json.Unmarshal(old.Terms, &p); e != nil {
			return PackageResult{}, e
		}
	}
	if c.Kind == "traffic" {
		snap, _, e := entitlementSnapshot(ctx, q, scope.TenantID(), c)
		if e != nil {
			return PackageResult{}, e
		}
		source, e := q.AppendTrafficSource(ctx, generated.AppendTrafficSourceParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SnapshotID: snap.ID, Kind: "traffic", InitialAmount: c.Amount, IssuedAt: timestamp(now), ExpiresAt: timestamp(now.AddDate(0, 0, int(c.DurationDays))), PeriodKey: key})
		if e != nil {
			return PackageResult{}, e
		}
		op, e := newDomainTask(ctx, q, scope, source.ID, "package.traffic.issued", "resource_change", 1)
		if e != nil {
			return PackageResult{}, e
		}
		return PackageResult{SnapshotID: snap.ID, SourceID: source.ID, OperationID: op.ID, Version: 1, AnchorAt: now, ExpiresAt: source.ExpiresAt.Time}, nil
	}
	live := exists && now.Before(sub.ExpiresAt.Time)
	if c.Kind == "duration" && !live {
		return PackageResult{}, ErrEntitlementInvalid
	}
	if live && old.PackageID != c.PackageID {
		return PackageResult{}, ErrPackageConfirmationRequired
	}
	if live {
		priorExpiry := sub.ExpiresAt.Time
		sub, e = q.ExtendSubscription(ctx, generated.ExtendSubscriptionParams{TenantID: scope.TenantID(), ExpiresAt: timestamp(priorExpiry.AddDate(0, 0, int(c.DurationDays))), Version: sub.Version})
		if e != nil {
			return PackageResult{}, e
		}
		// Preserve existing source IDs and balances. Expiry extension is a separate
		// immutable fact; it never changes amount or resurrects an expired source.
		rows, e := q.ListTrafficSources(ctx, scope.TenantID())
		if e != nil {
			return PackageResult{}, e
		}
		for _, r := range rows {
			if r.Kind != "base" || r.SnapshotID != sub.SnapshotID {
				continue
			}
			expiry, e := q.SourceEffectiveExpiry(ctx, generated.SourceEffectiveExpiryParams{TenantID: scope.TenantID(), ID: r.ID})
			if e != nil {
				return PackageResult{}, e
			}
			if !now.Before(expiry.Time) {
				continue
			}
			until := sub.ExpiresAt.Time
			if p.TrafficReset == "monthly" {
				_, boundary := MonthlyPeriod(sub.AnchorAt.Time, now)
				until = minTime(until, boundary)
			}
			if until.After(expiry.Time) {
				if e = q.AppendSourceExtension(ctx, generated.AppendSourceExtensionParams{TenantID: scope.TenantID(), SourceID: r.ID, RequestKey: key, ExpiresAt: timestamp(until)}); e != nil {
					return PackageResult{}, e
				}
			}
		}
		result := packageResult(sub, "", "")
		if c.Kind == "package" && p.TrafficReset == "none" {
			r, e := q.AppendTrafficSource(ctx, generated.AppendTrafficSourceParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SnapshotID: sub.SnapshotID, SubscriptionID: pgtype.Text{String: sub.ID, Valid: true}, Kind: "base", InitialAmount: c.Amount, IssuedAt: timestamp(now), ExpiresAt: sub.ExpiresAt, PeriodKey: key})
			if e != nil {
				return result, e
			}
			result.SourceID = r.ID
		}
		op, e := newDomainTask(ctx, q, scope, sub.ID, "package.extended", "resource_change", sub.Version)
		result.OperationID = op.ID
		return result, e
	}
	if c.Kind != "package" {
		return PackageResult{}, ErrEntitlementInvalid
	}
	if exists && old.PackageID != c.PackageID {
		return PackageResult{}, ErrPackageConfirmationRequired
	}
	snap, p, e := entitlementSnapshot(ctx, q, scope.TenantID(), c)
	if e != nil {
		return PackageResult{}, e
	}
	until := timestamp(now.AddDate(0, 0, int(c.DurationDays)))
	if !exists {
		sub, e = q.CreateSubscription(ctx, generated.CreateSubscriptionParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SnapshotID: snap.ID, AnchorAt: timestamp(now), ExpiresAt: until})
	} else {
		sub, e = q.ReplaceSubscription(ctx, generated.ReplaceSubscriptionParams{TenantID: scope.TenantID(), SnapshotID: snap.ID, AnchorAt: timestamp(now), ExpiresAt: until, Version: sub.Version})
	}
	if e != nil {
		return PackageResult{}, e
	}
	return issueBase(ctx, q, scope, sub, snap, p, now)
}
