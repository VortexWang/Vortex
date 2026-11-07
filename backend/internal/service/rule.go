package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type RuleInput = compiler.Rule
type RuleTarget = compiler.Target

var ErrRuleRevisionConflict = compiler.RevisionConflict

type RuleService struct{ Pool *pgxpool.Pool }

// CompileRevision is an internal producer boundary. Billing is supplied by the
// immutable billing-snapshot authority, never by public rule payloads. All node,
// grant and segment versions are loaded under this transaction's locks.
func (s RuleService) CompileRevision(ctx context.Context, scope TenantScope, id string, revision int64, billing compiler.BillingSnapshot, signerEpoch, pathGeneration, fencingGeneration int64, request string) (compiler.Result, error) {
	var result compiler.Result
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.compile", ResourceID: id, RequestID: request, Reason: "commit pinned compiler inputs and immutable per-node artifacts"}, func(q *generated.Queries) error {
		input, e := lockedCompileInput(ctx, q, scope, id, revision, billing, signerEpoch, pathGeneration, fencingGeneration)
		if e != nil {
			return e
		}
		result, e = repository.PersistCompilation(ctx, q, input)
		return e
	})
	if err != nil {
		return compiler.Result{}, err
	}
	return result, nil
}

func lockedCompileInput(ctx context.Context, q *generated.Queries, scope TenantScope, id string, revision int64, billing compiler.BillingSnapshot, signerEpoch, pathGeneration, fencingGeneration int64) (compiler.CompileInput, error) {
	if e := activeTenant(ctx, q, scope); e != nil {
		return compiler.CompileInput{}, e
	}
	head, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: id})
	if e != nil {
		return compiler.CompileInput{}, e
	}
	if head.DesiredRevision != revision {
		return compiler.CompileInput{}, ErrRuleRevisionConflict
	}
	rev, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: id, Revision: revision})
	if e != nil {
		return compiler.CompileInput{}, e
	}
	var rule RuleInput
	if e = json.Unmarshal(rev.Payload, &rule); e != nil {
		return compiler.CompileInput{}, e
	}
	locked, e := resolveRule(ctx, q, scope, rule)
	if e != nil {
		return compiler.CompileInput{}, e
	}
	input := compiler.CompileInput{TenantID: scope.TenantID(), Rule: rule, Revision: revision, Billing: billing, SignerEpoch: signerEpoch, PathGeneration: pathGeneration, FencingGeneration: fencingGeneration, ValidAt: rev.CreatedAt.Time.UTC(), Groups: []compiler.Group{}, Grants: []compiler.Grant{}, Permits: []compiler.Permit{}}
	for _, id := range rule.Path {
		g := locked.groups[id]
		snap := locked.members[id]
		group := compiler.Group{ID: id, OwnerKind: g.OwnerKind, Version: g.Version, MemberSetVersion: snap.MemberSetVersion, Scheduling: g.Scheduling, Members: []compiler.Member{}}
		for _, n := range snap.Members {
			group.Members = append(group.Members, compiler.Member{ID: n.ID, BootID: n.BootID, PublicAddress: n.PublicAddress, Version: n.Version, Generation: n.Generation, LinkPort: n.LinkPort, Weight: n.Weight, Priority: n.Priority, Online: n.Lifecycle == "online"})
		}
		input.Groups = append(input.Groups, group)
		if grant, ok := locked.grants[id]; ok {
			if grant.ValidFrom.Time.After(input.ValidAt) {
				input.ValidAt = grant.ValidFrom.Time.UTC()
			}
			input.Grants = append(input.Grants, compiler.Grant{ID: grant.ID, GroupID: id, Version: grant.Version, Active: grant.Status == "active", ValidFrom: grant.ValidFrom.Time, ValidUntil: grant.ValidUntil.Time})
		}
	}
	for i, v := range locked.permits {
		if v > 0 {
			input.Permits = append(input.Permits, compiler.Permit{From: rule.Path[i], To: rule.Path[i+1], Version: v, Allowed: true})
		}
	}
	allocations, e := q.RuleCompilePortAllocations(ctx, generated.RuleCompilePortAllocationsParams{TenantID: scope.TenantID(), RuleID: rule.ID, GroupID: rule.Path[0]})
	if e != nil {
		return compiler.CompileInput{}, e
	}
	if len(allocations) != 1 {
		return compiler.CompileInput{}, ErrConflict
	}
	if len(allocations) == 1 {
		a := allocations[0]
		if a.MemberSetVersion != locked.members[rule.Path[0]].MemberSetVersion {
			return compiler.CompileInput{}, ErrConflict
		}
		input.PortBinding = &compiler.PortBinding{ID: a.ID, GroupID: a.GroupID, MemberSetVersion: a.MemberSetVersion, Port: a.Port, ListenAddress: a.ListenAddress, Protocols: a.Protocols}
		if e = validateRulePortMembers(ctx, q, scope, input); e != nil {
			return compiler.CompileInput{}, e
		}
	}
	return input, nil
}

type RuleMutation struct {
	Revision    generated.RuleRevision
	OperationID string
	ChangeClass string
}
type ruleInputs struct {
	groups     map[string]generated.ScopedGroupRow
	members    map[string]MemberSnapshot
	directions []string
	permits    []int64
	grants     map[string]generated.ResourceGrant
}

// resolveRule locks every group/member in stable order, checks actual current
// authority, and retains every member. It never silently skips an empty hop.
func resolveRule(ctx context.Context, q *generated.Queries, scope TenantScope, in RuleInput) (ruleInputs, error) {
	r := ruleInputs{groups: map[string]generated.ScopedGroupRow{}, members: map[string]MemberSnapshot{}, grants: map[string]generated.ResourceGrant{}}
	if e := activeTenant(ctx, q, scope); e != nil {
		return r, e
	}
	ids := append([]string(nil), in.Path...)
	sort.Strings(ids)
	nodes := map[string]bool{}
	for _, id := range ids {
		g, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: id, TenantID: scope.TenantID(), PlatformRead: false})
		if e != nil {
			return r, e
		}
		r.groups[id] = g
		snap, e := snapshot(ctx, q, scope, id)
		if e != nil {
			return r, e
		}
		if len(snap.Members) == 0 {
			return r, compiler.Unreachable
		}
		r.members[id] = snap
		for _, n := range snap.Members {
			if nodes[n.ID] || n.Lifecycle != "online" || n.BootID == "" {
				return r, compiler.Unreachable
			}
			nodes[n.ID] = true
		}
		if g.OwnerKind == "platform" {
			grant, e := q.LatestGrant(ctx, generated.LatestGrantParams{TenantID: scope.TenantID(), ResourceID: id})
			if e != nil {
				return r, e
			}
			r.grants[id] = grant
		}
	}
	for i, id := range in.Path {
		want := "relay"
		if i == 0 {
			want = "entry"
		} else if i == len(in.Path)-1 {
			want = "exit"
		}
		if r.groups[id].Purpose != want {
			return r, compiler.Invalid
		}
	}
	for i, d := range in.Directions {
		from, to := in.Path[i], in.Path[i+1]
		public := func(group string) bool {
			for _, n := range r.members[group].Members {
				if n.PublicAddress == "" {
					return false
				}
			}
			return true
		}
		if d == "auto" {
			d = "forward"
			if !public(to) {
				d = "reverse"
			}
		}
		if (d == "forward" && !public(to)) || (d == "reverse" && !public(from)) {
			return r, compiler.Unreachable
		}
		// Manual concealment of an otherwise reachable downstream is a paid
		// capability. Until the entitlement producer supplies it, fail closed.
		if in.Directions[i] == "reverse" && public(to) {
			return r, compiler.Unauthorized
		}
		permit, e := q.GetSegmentPermit(ctx, generated.GetSegmentPermitParams{TenantID: scope.TenantID(), FromGroupID: from, ToGroupID: to})
		version := int64(0)
		if errors.Is(e, pgx.ErrNoRows) && r.groups[from].OwnerKind == "tenant" && r.groups[to].OwnerKind == "tenant" {
			e = nil
		} else if e == nil {
			if !permit.Allowed {
				return r, compiler.Unauthorized
			}
			version = permit.Version
		}
		if e != nil {
			return r, compiler.Unauthorized
		}
		r.directions = append(r.directions, d)
		r.permits = append(r.permits, version)
	}
	return r, nil
}

func (s RuleService) Mutate(ctx context.Context, scope TenantScope, in RuleInput, expected int64, request string) (RuleMutation, error) {
	return s.mutate(ctx, scope, in, expected, request, 0)
}
func (s RuleService) mutate(ctx context.Context, scope TenantScope, in RuleInput, expected int64, request string, rollbackOf int64) (RuleMutation, error) {
	var result RuleMutation
	in, e := compiler.NormalizeRule(in)
	if e != nil {
		return result, e
	}
	if expected < 0 {
		return result, ErrRuleRevisionConflict
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.revise", ResourceID: in.ID, RequestID: request, Reason: "append immutable expected revision"}, func(q *generated.Queries) error {
		var err error
		result, err = appendRuleRevision(ctx, q, scope, in, expected, rollbackOf)
		return err
	})
	return result, e
}

func appendRuleRevision(ctx context.Context, q *generated.Queries, scope TenantScope, in RuleInput, expected, rollbackOf int64) (RuleMutation, error) {
	var result RuleMutation
	err := func() error {
		if e := activeTenant(ctx, q, scope); e != nil {
			return e
		}
		if e := q.EnsureRuleHead(ctx, generated.EnsureRuleHeadParams{TenantID: scope.TenantID(), ID: in.ID}); e != nil {
			return e
		}
		head, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: in.ID})
		if e != nil {
			return e
		}
		if head.DesiredRevision != expected {
			return ErrRuleRevisionConflict
		}
		inputs, e := resolveRule(ctx, q, scope, in)
		if e != nil {
			return e
		}
		class := "rebuild"
		if expected > 0 {
			old, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected})
			if e != nil {
				return e
			}
			var prior RuleInput
			if e = json.Unmarshal(old.Payload, &prior); e != nil {
				return e
			}
			class = compiler.ChangeClass(prior, in)
		}
		body, e := json.Marshal(in)
		if e != nil {
			return e
		}
		digest := sha256.Sum256(body)
		result.Revision, e = q.AppendRuleRevision(ctx, generated.AppendRuleRevisionParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, Payload: body, PayloadSha256: digest[:], CreatedBy: scope.ActorID(), ChangeClass: class, RollbackOf: rollbackOf})
		if e != nil {
			return e
		}
		for _, target := range in.Targets {
			if e = q.InsertRuleTarget(ctx, generated.InsertRuleTargetParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, Address: target.Address, Weight: target.Weight, Priority: target.Priority}); e != nil {
				return e
			}
		}
		for i, d := range inputs.directions {
			if e = q.InsertRuleSegment(ctx, generated.InsertRuleSegmentParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, Hop: int32(i), FromGroupID: in.Path[i], ToGroupID: in.Path[i+1], Direction: d, PermitVersion: inputs.permits[i]}); e != nil {
				return e
			}
		}
		for _, id := range in.Path {
			if e = q.InsertRuleDependency(ctx, generated.InsertRuleDependencyParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, DependencyKind: "group", DependencyID: id, Version: inputs.members[id].MemberSetVersion}); e != nil {
				return e
			}
			if g, ok := inputs.grants[id]; ok {
				if e = q.InsertRuleDependency(ctx, generated.InsertRuleDependencyParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, DependencyKind: "grant", DependencyID: id, Version: strconv.FormatInt(g.Version, 10)}); e != nil {
					return e
				}
			}
		}
		for i, v := range inputs.permits {
			if e = q.InsertRuleDependency(ctx, generated.InsertRuleDependencyParams{TenantID: scope.TenantID(), RuleID: in.ID, Revision: expected + 1, DependencyKind: "permit", DependencyID: in.Path[i] + "/" + in.Path[i+1], Version: strconv.FormatInt(v, 10)}); e != nil {
				return e
			}
		}
		if e = q.SupersedePendingRuleCompiles(ctx, generated.SupersedePendingRuleCompilesParams{TenantID: scope.TenantID(), RuleID: in.ID}); e != nil {
			return e
		}
		obsolete, e := q.SupersedePendingRuleDeliveries(ctx, generated.SupersedePendingRuleDeliveriesParams{TenantID: scope.TenantID(), RuleID: in.ID, TargetRevision: expected + 1})
		if e != nil {
			return e
		}
		for _, task := range obsolete {
			if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task.OperationID, State: "superseded"}); e != nil {
				return e
			}
			if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: task.OperationID}); e != nil {
				return e
			}
		}
		if _, e = q.AdvanceRuleHead(ctx, generated.AdvanceRuleHeadParams{TenantID: scope.TenantID(), ID: in.ID, ExpectedRevision: expected}); e != nil {
			return e
		}
		op, e := newDomainTask(ctx, q, scope, in.ID, "rule.compile", "rule_compile", expected+1)
		if e != nil {
			return e
		}
		if e = q.BindRuleCompileTask(ctx, generated.BindRuleCompileTaskParams{TenantID: scope.TenantID(), TaskID: op.ID, RuleID: in.ID, RuleRevision: expected + 1}); e != nil {
			return e
		}
		result.OperationID = op.ID
		result.ChangeClass = class
		return nil
	}()
	if err != nil {
		return RuleMutation{}, err
	}
	return result, nil
}

// Invalidations are queued in the dependency mutation's existing transaction.
// Active revision/port/share authorization is deliberately untouched.
func invalidateRules(ctx context.Context, q *generated.Queries, scope TenantScope, kind, id string) error {
	rows, e := q.DependentRuleHeads(ctx, generated.DependentRuleHeadsParams{TenantID: scope.TenantID(), DependencyKind: kind, DependencyID: id})
	if e != nil {
		return e
	}
	for _, r := range rows {
		if e = q.SupersedePendingRuleCompiles(ctx, generated.SupersedePendingRuleCompilesParams{TenantID: scope.TenantID(), RuleID: r.ID}); e != nil {
			return e
		}
		op, err := newDomainTask(ctx, q, scope, r.ID, "rule.recompile", "rule_compile", r.DesiredRevision)
		if err != nil {
			return err
		}
		if e = q.BindRuleCompileTask(ctx, generated.BindRuleCompileTaskParams{TenantID: scope.TenantID(), TaskID: op.ID, RuleID: r.ID, RuleRevision: r.DesiredRevision}); e != nil {
			return e
		}
	}
	return nil
}
