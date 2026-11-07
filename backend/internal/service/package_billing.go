package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// ProduceBillingSnapshot pins explicit package, path revision and the ordered
// policy vector. It does not issue credit, signer epochs, leases or shares.
// Those must come from the later funded authority, never invented defaults.
func (s PackageService) ProduceBillingSnapshot(ctx context.Context, scope TenantScope, rule string, revision, path int64, request string) (compiler.BillingSnapshot, error) {
	var result compiler.BillingSnapshot
	if revision < 1 || path < 1 {
		return result, ErrConflict
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "package.billing_snapshot", ResourceID: rule, RequestID: request, Reason: "pin immutable package and ordered path policy"}, func(q *generated.Queries) error {
		if e := activeTenant(ctx, q, scope); e != nil {
			return e
		}
		sub, e := q.LockSubscription(ctx, scope.TenantID())
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
		var p PackageTerms
		if e = json.Unmarshal(snap.Terms, &p); e != nil {
			return e
		}
		head, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: rule})
		if e != nil {
			return e
		}
		if head.DesiredRevision != revision {
			return ErrRuleRevisionConflict
		}
		rev, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: rule, Revision: revision})
		if e != nil {
			return e
		}
		var in RuleInput
		if e = json.Unmarshal(rev.Payload, &in); e != nil {
			return e
		}
		if in.Enabled && !rulePermitted(in, p) {
			return compiler.Unauthorized
		}
		if in.Enabled {
			if e = activeRule(ctx, q, scope, rule); e != nil {
				return e
			}
		}
		old, e := q.FindBillingSnapshot(ctx, generated.FindBillingSnapshotParams{TenantID: scope.TenantID(), RuleID: rule, RuleRevision: revision, PathGeneration: path, PackageSnapshotID: snap.ID})
		if e == nil {
			result = billingModel(old)
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		resolved, e := resolveRule(ctx, q, scope, in)
		if e != nil {
			return e
		}
		type policyVersion struct {
			Group      string `json:"group_id"`
			Version    int64  `json:"version"`
			Multiplier int64  `json:"multiplier_milli"`
		}
		vector := []policyVersion{}
		var coefficient int64
		for _, id := range in.Path {
			g := resolved.groups[id]
			k := g.BillingMultiplierMilli
			if g.OwnerKind == "tenant" && k != 0 {
				return ErrConflict
			}
			if g.OwnerKind == "platform" && k < 1 {
				return ErrConflict
			}
			if k > math.MaxInt64-coefficient {
				return ErrConflict
			}
			coefficient += k
			vector = append(vector, policyVersion{id, g.Version, k})
		}
		row, e := q.AppendBillingSnapshot(ctx, generated.AppendBillingSnapshotParams{ID: uuid.NewString(), TenantID: scope.TenantID(), PackageSnapshotID: snap.ID, RuleID: rule, RuleRevision: revision, PathGeneration: path, MultiplierMilli: coefficient, PathVersions: auditJSON(vector)})
		if e == nil {
			result = billingModel(row)
		}
		return e
	})
	return result, e
}
func billingModel(b generated.BillingSnapshot) compiler.BillingSnapshot {
	return compiler.BillingSnapshot{ID: b.ID, Version: b.Version, MultiplierMilli: b.MultiplierMilli, RuleRevision: b.RuleRevision, PathGeneration: b.PathGeneration}
}
func rulePermitted(r RuleInput, p PackageTerms) bool {
	if !hasCapability(p, r.Mode) || ((r.Protocol == "tcp" || r.Protocol == "tcp_udp") && !hasCapability(p, "tcp")) || ((r.Protocol == "udp" || r.Protocol == "tcp_udp") && !hasCapability(p, "udp")) {
		return false
	}
	return (r.RateLimit == 0 || r.RateLimit <= p.RateLimit) && (r.ConnectionLimit == 0 || r.ConnectionLimit <= p.ConnectionLimit)
}

func validateBillingSnapshot(ctx context.Context, q *generated.Queries, scope TenantScope, rule string, b compiler.BillingSnapshot) error {
	stored, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: b.ID})
	if e != nil {
		return e
	}
	if stored.RuleID != rule || billingModel(stored) != b {
		return ErrConflict
	}
	return nil
}

// BindPackageCompile uses the durable producer and the same validated binding
// consumer used by the production worker. Explicit generations remain required.
func (s PackageService) BindPackageCompile(ctx context.Context, scope TenantScope, task string, signer, path, fence int64, request string) (string, error) {
	b, e := generated.New(s.Pool).GetRuleCompileRequest(ctx, generated.GetRuleCompileRequestParams{TenantID: scope.TenantID(), TaskID: task})
	if e != nil {
		return "", e
	}
	snapshot, e := s.ProduceBillingSnapshot(ctx, scope, b.RuleID, b.RuleRevision, path, request+"/snapshot")
	if e != nil {
		return "", e
	}
	return (RuleService{Pool: s.Pool}).BindCompileInput(ctx, scope, task, snapshot, signer, path, fence, request)
}
