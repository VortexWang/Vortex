package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
)

type RuleRepository struct{ Pool *pgxpool.Pool }

func (r RuleRepository) Revision(ctx context.Context, s TenantScope, id string, revision int64, request string) (generated.RuleRevision, error) {
	var result generated.RuleRevision
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "rule.read", ResourceID: id, RequestID: request, Reason: "scoped immutable rule revision"}, func(q *generated.Queries) error {
		var e error
		result, e = q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: s.TenantID(), RuleID: id, Revision: revision})
		return e
	})
	if err != nil {
		return generated.RuleRevision{}, err
	}
	return result, nil
}

// PersistCompilation is called only inside the service's locked authority and
// AuditWriter transaction. Recompute rather than trusting a caller's digest.
func PersistCompilation(ctx context.Context, q *generated.Queries, in compiler.CompileInput) (compiler.Result, error) {
	result, e := PersistCompileInput(ctx, q, in)
	if e != nil {
		return compiler.Result{}, e
	}
	for _, a := range result.Artifacts {
		if e = q.InsertRuleArtifact(ctx, generated.InsertRuleArtifactParams{TenantID: in.TenantID, RuleID: in.Rule.ID, RuleRevision: in.Revision, NodeID: a.NodeID, InputDigest: result.InputDigest, Digest: a.Digest, Canonical: a.Canonical, BillingSnapshotID: in.Billing.ID, SignerEpoch: in.SignerEpoch, PathGeneration: in.PathGeneration, FencingGeneration: in.FencingGeneration}); e != nil {
			return compiler.Result{}, e
		}
		if e = q.CommitRuleArtifact(ctx, generated.CommitRuleArtifactParams{TenantID: in.TenantID, Digest: a.Digest}); e != nil {
			return compiler.Result{}, e
		}
	}
	return result, nil
}

// PersistCompileInput validates and pins the complete input without creating a
// deliverable artifact. The durable task consumer commits artifacts later.
func PersistCompileInput(ctx context.Context, q *generated.Queries, in compiler.CompileInput) (compiler.Result, error) {
	result := compiler.Compile(ctx, in)
	if len(result.Diagnostics) != 0 {
		return result, result.Diagnostics[0]
	}
	revision, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: in.TenantID, RuleID: in.Rule.ID, Revision: in.Revision})
	if e != nil {
		return compiler.Result{}, e
	}
	rule, e := compiler.NormalizeRule(in.Rule)
	if e != nil {
		return compiler.Result{}, e
	}
	raw, e := json.Marshal(rule)
	if e != nil || !bytes.Equal(raw, revision.Payload) {
		return compiler.Result{}, compiler.RevisionConflict
	}
	if e = q.InsertRuleCompileInput(ctx, generated.InsertRuleCompileInputParams{TenantID: in.TenantID, RuleID: in.Rule.ID, RuleRevision: in.Revision, InputDigest: result.InputDigest, Canonical: result.InputCanonical, BillingSnapshotID: in.Billing.ID, SignerEpoch: in.SignerEpoch, PathGeneration: in.PathGeneration, FencingGeneration: in.FencingGeneration}); e != nil {
		return compiler.Result{}, e
	}
	return result, nil
}

type ArtifactBinding struct {
	RuleID, NodeID, Digest      string
	Revision, FencingGeneration int64
}

func CommittedArtifact(ctx context.Context, q *generated.Queries, tenant string, b ArtifactBinding) (compiler.Envelope, error) {
	a, e := q.GetCommittedRuleArtifact(ctx, generated.GetCommittedRuleArtifactParams{TenantID: tenant, Digest: b.Digest, RuleID: b.RuleID, RuleRevision: b.Revision, NodeID: b.NodeID, FencingGeneration: b.FencingGeneration})
	if e != nil {
		return compiler.Envelope{}, e
	}
	env, e := compiler.DecodeArtifact(a.Canonical, a.Digest)
	if e != nil {
		return compiler.Envelope{}, e
	}
	if env.TenantID != tenant || env.RuleID != b.RuleID || env.RuleRevision != b.Revision || env.NodeID != b.NodeID || env.FencingGeneration != b.FencingGeneration || env.InputDigest != a.InputDigest || env.SignerEpoch != a.SignerEpoch || env.PathGeneration != a.PathGeneration || env.BillingSnapshotID != a.BillingSnapshotID {
		return compiler.Envelope{}, errors.New("artifact binding mismatch")
	}
	return env, nil
}
