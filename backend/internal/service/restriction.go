package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type packageDiscard struct {
	SourceID               string `json:"source_id"`
	IssuedAmountUpperBound int64  `json:"issued_amount_upper_bound"`
	Disposition            string `json:"disposition"`
}
type packageRuleLoss struct {
	RuleID string `json:"rule_id"`
	Reason string `json:"reason"`
}
type downgradePreview struct {
	Digest              string              `json:"digest"`
	PackageID           string              `json:"package_id"`
	PackageVersion      int64               `json:"package_version"`
	SubscriptionVersion int64               `json:"subscription_version"`
	EffectiveAt         time.Time           `json:"effective_at"`
	KeepRules           []string            `json:"keep_rules"`
	LostCapabilities    []string            `json:"lost_capabilities"`
	InvalidRules        []string            `json:"invalid_rules"`
	NewRestrictions     []packageRuleLoss   `json:"new_restrictions"`
	DiscardedAmounts    []packageDiscard    `json:"discarded_amounts"`
	AffectedResources   []string            `json:"affected_resources"`
	Entitlement         *entitlementBinding `json:"entitlement,omitempty"`
}

type entitlementBinding struct {
	CodeHash     string `json:"code_hash"`
	ActorID      string `json:"actor_id"`
	DurationDays int32  `json:"duration_days"`
	Amount       int64  `json:"amount"`
}

func bindEntitlement(p downgradePreview, b *entitlementBinding) downgradePreview {
	p.Entitlement = b
	d := sha256.Sum256(auditJSON(p))
	p.Digest = hex.EncodeToString(d[:])
	return p
}

// A stale in-memory scope never authorizes a new package transition. The actor
// is reloaded and locked until commit; delayed execution rechecks it again.
func packageActor(ctx context.Context, q *generated.Queries, scope TenantScope, action policy.Action) error {
	var role, state string
	if scope.Platform() {
		row, e := q.LockPackagePlatformActor(ctx, scope.ActorID())
		if e != nil {
			return ErrScope
		}
		role, state = row.Role, row.State
	} else {
		row, e := q.LockPackageActor(ctx, generated.LockPackageActorParams{TenantID: scope.TenantID(), ID: scope.ActorID()})
		if e != nil {
			return ErrScope
		}
		role, state = row.Role, row.State
	}
	home := scope.TenantID()
	if scope.Platform() {
		home = ""
	}
	principal, e := auth.NewPrincipal(scope.ActorID(), home, auth.Role(role), state)
	if e != nil {
		return ErrScope
	}
	if e = policy.Authorize(principal, scope.TenantID(), action); e != nil {
		return ErrScope
	}
	return nil
}

func activeRule(ctx context.Context, q *generated.Queries, scope TenantScope, id string) error {
	if e := activeTenant(ctx, q, scope); e != nil {
		return e
	}
	restricted, e := q.RuleHasPackageRestriction(ctx, generated.RuleHasPackageRestrictionParams{TenantID: scope.TenantID(), ResourceID: pgtype.Text{String: id, Valid: true}, AtTime: timestamp(time.Now())})
	if e != nil {
		return e
	}
	if restricted {
		return ErrConflict
	}
	return nil
}

func rulePackageTerms(ctx context.Context, q *generated.Queries, scope TenantScope, r RuleInput) error {
	if !r.Enabled {
		return nil
	}
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if !time.Now().Before(sub.ExpiresAt.Time) {
		return ErrConflict
	}
	snap, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: sub.SnapshotID})
	if e != nil {
		return e
	}
	var terms PackageTerms
	if e = json.Unmarshal(snap.Terms, &terms); e != nil {
		return e
	}
	if !rulePermitted(r, terms) {
		return ErrConflict
	}
	rows, e := q.TenantPackageRules(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	count := int64(1)
	for _, row := range rows {
		if row.ID == r.ID {
			continue
		}
		var other RuleInput
		if e = json.Unmarshal(row.Payload, &other); e != nil {
			return e
		}
		if other.Enabled {
			count++
		}
	}
	if count > terms.RuleLimit {
		return ErrConflict
	}
	return nil
}

// Preview uses one repeatable-read, read-only database snapshot. It does not
// persist a preview token, issue rights, audit, or create tasks. Its digest pins
// every rule/source/port/restriction input, not merely the subscription version.
func (s PackageService) PreviewDowngrade(ctx context.Context, scope TenantScope, pkg string, version int64, keep []string, at time.Time) (json.RawMessage, error) {
	if !scope.Allows(policy.Read) || scope.Platform() {
		return nil, ErrScope
	}
	var result downgradePreview
	e := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		result, e = previewDowngrade(ctx, generated.New(tx), scope, pkg, version, keep, at)
		return e
	})
	if e != nil {
		return nil, e
	}
	return json.Marshal(result)
}

func previewDowngrade(ctx context.Context, q *generated.Queries, scope TenantScope, pkg string, version int64, keep []string, at time.Time) (downgradePreview, error) {
	out := downgradePreview{PackageID: pkg, PackageVersion: version, EffectiveAt: at.UTC().Truncate(time.Microsecond), KeepRules: append([]string{}, keep...), LostCapabilities: []string{}, InvalidRules: []string{}, NewRestrictions: []packageRuleLoss{}, DiscardedAmounts: []packageDiscard{}, AffectedResources: []string{}}
	if !validID(pkg) || version < 1 || at.IsZero() || len(keep) > 10000 {
		return out, ErrConflict
	}
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	out.SubscriptionVersion = sub.Version
	old, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: sub.SnapshotID})
	if e != nil {
		return out, e
	}
	target, e := q.GetPackageVersion(ctx, generated.GetPackageVersionParams{PackageID: pkg, Version: version})
	if e != nil {
		return out, e
	}
	var current, next PackageTerms
	if e = json.Unmarshal(old.Terms, &current); e != nil {
		return out, e
	}
	if e = json.Unmarshal(target.Terms, &next); e != nil {
		return out, e
	}
	for _, c := range current.Capabilities {
		if !hasCapability(next, c) {
			out.LostCapabilities = append(out.LostCapabilities, c)
		}
	}
	rules, e := q.TenantPackageRules(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	sources, e := q.ListTrafficSources(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	ports, e := q.TenantPackagePorts(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	restrictions, e := q.ListRestrictionHistory(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	tenant, e := q.GetTenant(ctx, scope.TenantID())
	if e != nil {
		return out, e
	}
	all := map[string]RuleInput{}
	order := []string{}
	for _, row := range rules {
		var r RuleInput
		if e = json.Unmarshal(row.Payload, &r); e != nil {
			return out, e
		}
		all[r.ID] = r
		if r.Enabled {
			order = append(order, r.ID)
		}
	}
	seen := map[string]bool{}
	priority := []string{}
	for _, id := range keep {
		r, ok := all[id]
		if !ok || !r.Enabled || seen[id] {
			return out, ErrConflict
		}
		seen[id] = true
		priority = append(priority, id)
	}
	for _, id := range order {
		if !seen[id] {
			priority = append(priority, id)
		}
	}
	count := int64(0)
	invalid := map[string]bool{}
	for _, id := range priority {
		r := all[id]
		reason := ""
		if !rulePermitted(r, next) {
			reason = "capability_or_limit_lost"
		} else if count >= next.RuleLimit {
			reason = "quantity_exceeded"
		} else {
			count++
		}
		if reason != "" {
			invalid[id] = true
			out.InvalidRules = append(out.InvalidRules, id)
			out.NewRestrictions = append(out.NewRestrictions, packageRuleLoss{id, reason})
		}
	}
	for _, a := range ports {
		if invalid[a.RuleID] {
			out.AffectedResources = append(out.AffectedResources, a.ID)
		}
	}
	// These are issuance upper bounds, not fabricated available balances. The
	// funded ledger in 06-05 retains reservations and all unknown liabilities.
	type sourceExpiry struct {
		ID      string
		Expiry  time.Time
		Expired bool
	}
	expiries := []sourceExpiry{}
	for _, row := range sources {
		expiry, e := q.SourceEffectiveExpiry(ctx, generated.SourceEffectiveExpiryParams{TenantID: scope.TenantID(), ID: row.ID})
		if e != nil {
			return out, e
		}
		expired, e := q.SourceIsExpired(ctx, generated.SourceIsExpiredParams{TenantID: scope.TenantID(), SourceID: row.ID, AtTime: timestamp(at)})
		if e != nil {
			return out, e
		}
		expiries = append(expiries, sourceExpiry{row.ID, expiry.Time, expired})
		if row.Kind == "base" && !expired && at.Before(expiry.Time) {
			out.DiscardedAmounts = append(out.DiscardedAmounts, packageDiscard{row.ID, row.InitialAmount, "expire_source_without_releasing_liability"})
		}
	}
	sort.Strings(out.InvalidRules)
	sort.Strings(out.AffectedResources)
	sort.Strings(out.LostCapabilities)
	input := struct {
		Tenant       generated.Tenant
		Subscription generated.Subscription
		Old          generated.PackageSnapshot
		Target       generated.PackageVersion
		Rules        []generated.TenantPackageRulesRow
		Sources      []generated.TrafficSource
		Expiries     []sourceExpiry
		Ports        []generated.PortAllocation
		Restrictions []generated.RestrictionEvent
		Preview      downgradePreview
	}{tenant, sub, old, target, rules, sources, expiries, ports, restrictions, out}
	digest := sha256.Sum256(auditJSON(input))
	out.Digest = hex.EncodeToString(digest[:])
	return out, nil
}

func (s PackageService) ConfirmDowngrade(ctx context.Context, scope TenantScope, pkg string, version int64, keep []string, at time.Time, digest, key, request string) (PackageResult, error) {
	return s.confirmPackageChange(ctx, scope, pkg, version, "", keep, at, digest, key, request, nil)
}

// ConfirmDowngradeAtVersion binds public If-Match inside the same transaction
// as immutable preview validation and accepted task/audit persistence.
func (s PackageService) ConfirmDowngradeAtVersion(ctx context.Context, scope TenantScope, pkg string, version, expected int64, keep []string, at time.Time, digest, key, request string) (PackageResult, error) {
	if expected < 1 {
		return PackageResult{}, ErrPrecondition
	}
	return s.confirmPackageChange(ctx, scope, pkg, version, "", keep, at, digest, key, request, &expected)
}

func (s PackageService) confirmPackageChange(ctx context.Context, scope TenantScope, pkg string, version int64, code string, keep []string, at time.Time, digest, key, request string, expected *int64) (PackageResult, error) {
	var result PackageResult
	if !scope.Allows(policy.Mutate) || scope.Platform() {
		return result, ErrScope
	}
	at = at.UTC().Truncate(time.Microsecond)
	payload := struct {
		Kind, Package   string
		Actor           string
		CodeHash        string
		Version         int64
		Keep            []string
		At              time.Time
		Digest          string
		ExpectedVersion *int64 `json:"ExpectedVersion,omitempty"`
	}{Kind: "package_change", Package: pkg, Actor: scope.ActorID(), Version: version, Keep: append([]string{}, keep...), At: at, Digest: digest, ExpectedVersion: expected}
	if code != "" {
		hash := sha256.Sum256([]byte(code))
		payload.Kind, payload.CodeHash = "entitlement_change", hex.EncodeToString(hash[:])
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "package.change.confirm", ResourceID: scope.TenantID(), RequestID: request, Reason: "confirm fixed downgrade inputs and explicit effective time"}, func(q *generated.Queries) error {
		if e := packageActor(ctx, q, scope, policy.Mutate); e != nil {
			return e
		}
		replay, e := requestReplay(ctx, q, scope.TenantID(), key, payload, &result)
		if e != nil || replay {
			return e
		}
		if at.Before(time.Now().UTC().Truncate(time.Microsecond)) || len(digest) != 64 {
			return ErrConflict
		}
		if _, e = q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		sub, e := q.LockSubscription(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		if expected != nil && sub.Version != *expected {
			return ErrPrecondition
		}
		var c generated.EntitlementCode
		if code != "" {
			c, e = lockedCode(ctx, q, scope.TenantID(), code)
			if e != nil {
				return e
			}
			if c.Kind != "package" {
				return ErrEntitlementInvalid
			}
			pkg, version = c.PackageID, c.PackageVersion
		}
		preview, e := previewDowngrade(ctx, q, scope, pkg, version, keep, at)
		if e != nil {
			return e
		}
		if code != "" {
			preview = bindEntitlement(preview, codeBinding(c, scope.ActorID()))
		}
		if preview.Digest != digest {
			return ErrConflict
		}
		var snap generated.PackageSnapshot
		var p PackageTerms
		if code != "" {
			snap, p, e = entitlementSnapshot(ctx, q, scope.TenantID(), c)
		} else {
			snap, p, e = packageSnapshot(ctx, q, scope.TenantID(), pkg, version)
		}
		if e != nil {
			return e
		}
		id := uuid.NewString()
		op, e := newDomainTask(ctx, q, scope, id, "package.change.due", "package_due", sub.Version+1)
		if e != nil {
			return e
		}
		if _, e = q.AppendPackageChange(ctx, generated.AppendPackageChangeParams{ID: id, TenantID: scope.TenantID(), SubscriptionID: sub.ID, SnapshotID: snap.ID, ExpectedVersion: sub.Version, EffectiveAt: timestamp(at), Preview: auditJSON(preview), CreatedBy: scope.ActorID(), TaskID: op.ID}); e != nil {
			var conflict *pgconn.PgError
			if errors.As(e, &conflict) && conflict.Code == "23505" && conflict.ConstraintName == "one_pending_package_change" {
				return ErrConflict
			}
			return e
		}
		result = PackageResult{SubscriptionID: sub.ID, SnapshotID: snap.ID, OperationID: op.ID, Version: sub.Version + 1, AnchorAt: at, ExpiresAt: at.AddDate(0, 0, int(p.DurationDays))}
		if e = savePackageRequest(ctx, q, scope.TenantID(), key, payload, result); e != nil {
			return e
		}
		if code != "" {
			n, e := q.ConsumeEntitlementCode(ctx, c.CodeHash)
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrEntitlementInvalid
			}
			return q.AppendEntitlementRedemption(ctx, generated.AppendEntitlementRedemptionParams{RequestKey: key, TenantID: scope.TenantID(), CodeHash: c.CodeHash, Result: auditJSON(result)})
		}
		return nil
	})
	if e != nil {
		return PackageResult{}, e
	}
	return result, nil
}

// ProcessDuePackageChanges is the trusted clock boundary used by the production
// worker. No user endpoint accepts this clock. Each change is serialized on its
// tenant and change rows, and the entire apply rolls back on audit/DB failure.
func (s PackageService) ProcessDuePackageChanges(ctx context.Context, at time.Time) (bool, error) {
	rows, e := generated.New(s.Pool).DuePackageChanges(ctx, timestamp(at))
	if e != nil {
		return false, e
	}
	for _, row := range rows {
		scope, e := NewTenantScope(row.CreatedBy, row.TenantID, row.TenantID)
		if e != nil {
			return false, e
		}
		e = s.applyPackageChange(ctx, scope, row, at)
		if errors.Is(e, ErrConflict) || errors.Is(e, ErrScope) || errors.Is(e, ErrRuleRevisionConflict) {
			e = s.rejectPackageChange(ctx, scope, row)
		}
		return true, e
	}
	return false, nil
}

func (s PackageService) applyPackageChange(ctx context.Context, scope TenantScope, row generated.PackageChange, at time.Time) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "package.change.apply", ResourceID: row.ID, RequestID: row.ID, Reason: "apply persisted due change; never release unproven occupation"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		change, e := q.LockPackageChange(ctx, generated.LockPackageChangeParams{TenantID: scope.TenantID(), ID: row.ID})
		if e != nil {
			return e
		}
		if change.State != "pending" {
			return nil
		}
		if at.Before(change.EffectiveAt.Time) {
			return ErrConflict
		}
		if e = packageActor(ctx, q, scope, policy.Mutate); e != nil {
			return e
		}
		var stored downgradePreview
		if e = json.Unmarshal(change.Preview, &stored); e != nil {
			return e
		}
		fresh, e := previewDowngrade(ctx, q, scope, stored.PackageID, stored.PackageVersion, stored.KeepRules, stored.EffectiveAt)
		if e != nil {
			return e
		}
		if stored.Entitlement != nil {
			if stored.Entitlement.ActorID != scope.ActorID() {
				return ErrScope
			}
			fresh = bindEntitlement(fresh, stored.Entitlement)
		}
		if fresh.Digest != stored.Digest {
			return ErrConflict
		}
		// Append real disabled revisions before installing local restrictions. Their
		// compile tasks use the existing durable billing/funded producer boundary.
		rules, e := q.TenantPackageRules(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		invalid := map[string]bool{}
		for _, id := range stored.InvalidRules {
			invalid[id] = true
		}
		for _, r := range rules {
			if !invalid[r.ID] {
				continue
			}
			var rule RuleInput
			if e = json.Unmarshal(r.Payload, &rule); e != nil {
				return e
			}
			rule.Enabled = false
			if _, e = appendRuleRevision(ctx, q, scope, rule, r.DesiredRevision, 0); e != nil {
				return e
			}
		}
		sub, e := q.LockSubscription(ctx, scope.TenantID())
		if e != nil {
			return e
		}
		if sub.Version != change.ExpectedVersion {
			return ErrConflict
		}
		snap, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: change.SnapshotID})
		if e != nil {
			return e
		}
		var p PackageTerms
		if e = json.Unmarshal(snap.Terms, &p); e != nil {
			return e
		}
		sub, e = q.ReplaceSubscription(ctx, generated.ReplaceSubscriptionParams{TenantID: scope.TenantID(), SnapshotID: snap.ID, AnchorAt: change.EffectiveAt, ExpiresAt: timestamp(stored.EffectiveAt.AddDate(0, 0, int(p.DurationDays))), Version: sub.Version})
		if e != nil {
			return e
		}
		for _, d := range stored.DiscardedAmounts {
			if e = q.ExpireSource(ctx, generated.ExpireSourceParams{TenantID: scope.TenantID(), SourceID: d.SourceID, EffectiveAt: change.EffectiveAt, Reason: "replacement"}); e != nil {
				return e
			}
		}
		if _, e = issueBase(ctx, q, scope, sub, snap, p, stored.EffectiveAt); e != nil {
			return e
		}
		if e = applyPackageRuleLosses(ctx, q, scope, stored, snap.ID, sub.Version); e != nil {
			return e
		}
		if e = q.FinishPackageChange(ctx, generated.FinishPackageChangeParams{TenantID: scope.TenantID(), ID: change.ID, State: "succeeded"}); e != nil {
			return e
		}
		return finishPackageDueTask(ctx, q, scope, change.TaskID, "succeeded")
	})
}

func applyPackageRuleLosses(ctx context.Context, q *generated.Queries, scope TenantScope, p downgradePreview, snapshot string, version int64) error {
	old, e := q.PackageRuleRestrictions(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	for _, r := range old {
		if e = q.ClearPackageRuleRestriction(ctx, generated.ClearPackageRuleRestrictionParams{ID: uuid.NewString(), TenantID: scope.TenantID(), EventID: r.EventID, ClearedAt: timestamp(p.EffectiveAt)}); e != nil {
			return e
		}
	}
	invalid := map[string]bool{}
	for _, id := range p.InvalidRules {
		invalid[id] = true
		if e = q.ApplyPackageRuleRestriction(ctx, generated.ApplyPackageRuleRestrictionParams{ID: uuid.NewString(), TenantID: scope.TenantID(), SourceID: snapshot + "/" + id, SourceVersion: version, EffectiveAt: timestamp(p.EffectiveAt), ResourceID: pgtype.Text{String: id, Valid: true}}); e != nil {
			return e
		}
	}
	ports, e := q.TenantPackagePorts(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	for _, a := range ports {
		if !invalid[a.RuleID] || a.State == "releasing" || a.State == "aborting" {
			continue
		}
		if _, e = lockAllocation(ctx, q, scope, a.ID); e != nil {
			return e
		}
		if e = q.RetainPortAllocation(ctx, generated.RetainPortAllocationParams{TenantID: scope.TenantID(), ID: a.ID, EffectiveAt: timestamp(p.EffectiveAt)}); e != nil {
			return e
		}
		// Unknown receipts stay unknown; retention never turns them into free or
		// confirmed capacity. Only the dedicated proof path may release them.
		if e = q.RetainPackageReservations(ctx, generated.RetainPackageReservationsParams{TenantID: scope.TenantID(), AllocationID: a.ID}); e != nil {
			return e
		}
	}
	return nil
}
func finishPackageDueTask(ctx context.Context, q *generated.Queries, scope TenantScope, id, state string) error {
	if e := q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: id, State: state}); e != nil {
		return e
	}
	if e := q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: id, State: state}); e != nil {
		return e
	}
	return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: id})
}
func (s PackageService) rejectPackageChange(ctx context.Context, scope TenantScope, row generated.PackageChange) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "package.change.failed", ResourceID: row.ID, RequestID: row.ID + "/failed", Reason: "stale inputs or revoked actor; no partial authority transition"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		change, e := q.LockPackageChange(ctx, generated.LockPackageChangeParams{TenantID: scope.TenantID(), ID: row.ID})
		if e != nil {
			return e
		}
		if change.State != "pending" {
			return nil
		}
		if e = q.FinishPackageChange(ctx, generated.FinishPackageChangeParams{TenantID: scope.TenantID(), ID: row.ID, State: "failed"}); e != nil {
			return e
		}
		return finishPackageDueTask(ctx, q, scope, row.TaskID, "failed")
	})
}
