package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type NodeService struct{ Pool *pgxpool.Pool }
type NodeInput struct {
	ID, Name, Region, HubID, OwnerKind, PublicAddress, ProgramVersion string
	Capabilities                                                      []string
	PortMin, PortMax, LinkPort, ControlPort, MetricsPort              int32
	Capacity                                                          int64
}
type GroupInput struct {
	ID, Name, OwnerKind, Purpose, Scheduling    string
	HealthIntervalSeconds, HealthTimeoutSeconds int32
	BillingMultiplierMilli                      int64
}
type GroupMember struct {
	NodeID           string
	Weight, Priority int32
}
type MemberSnapshot struct {
	GroupID, MemberSetVersion string
	GroupVersion              int64
	Members                   []generated.GroupMembersRow
}

func validID(s string) bool      { _, err := uuid.Parse(s); return err == nil }
func auditJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
func owns(scope TenantScope, kind string, tenant pgtype.Text) bool {
	return (kind == "platform" && scope.Platform()) || (kind == "tenant" && tenant.Valid && tenant.String == scope.TenantID())
}
func ownership(scope TenantScope, kind string) (pgtype.Text, error) {
	if !scope.Allows(policy.Mutate) {
		return pgtype.Text{}, ErrScope
	}
	if kind == "platform" && scope.Platform() {
		return pgtype.Text{}, nil
	}
	if kind == "tenant" {
		return pgtype.Text{String: scope.TenantID(), Valid: true}, nil
	}
	return pgtype.Text{}, ErrScope
}
func domainTask(ctx context.Context, q *generated.Queries, scope TenantScope, resource, action, kind string, version int64) error {
	_, err := newDomainTask(ctx, q, scope, resource, action, kind, version)
	return err
}

// Domain events get their own desired-state identity. They must never overwrite
// a node's RuntimeConfig or become eligible for restriction-resume delivery.
func newDomainTask(ctx context.Context, q *generated.Queries, scope TenantScope, resource, action, kind string, version int64) (generated.Operation, error) {
	zero := generated.Operation{}
	body, err := json.Marshal(struct {
		Action   string `json:"action"`
		Resource string `json:"resource_id"`
		Version  int64  `json:"version"`
	}{action, resource, version})
	if err != nil {
		return zero, err
	}
	id := uuid.NewString()
	revision := int64(1)
	digest := sha256.Sum256(body)
	if _, err = q.PutDesiredState(ctx, generated.PutDesiredStateParams{TenantID: scope.TenantID(), ResourceID: id, TargetKind: "node", Revision: revision, ExpectedRevision: revision - 1, Payload: body, PayloadSha256: digest[:]}); err != nil {
		return zero, err
	}
	op, err := q.InsertOperation(ctx, generated.InsertOperationParams{ID: id, TenantID: scope.TenantID(), ResourceID: id, IdempotencyKey: id, RequestSha256: digest[:], TargetKind: "node", TargetRevision: revision, ExpectedRevision: revision - 1, Generation: version, Payload: body})
	if err != nil {
		return zero, err
	}
	if err = q.InsertDomainTask(ctx, generated.InsertDomainTaskParams{ID: id, TenantID: scope.TenantID(), OperationID: id, TaskKind: kind}); err != nil {
		return zero, err
	}
	err = q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: scope.TenantID(), OperationID: id})
	return op, err
}

func (s NodeService) CreateNode(ctx context.Context, scope TenantScope, in NodeInput, request string) (generated.Node, error) {
	var result generated.Node
	tenant, err := ownership(scope, in.OwnerKind)
	if err != nil {
		return result, err
	}
	if !validID(in.ID) || !validID(in.HubID) {
		return result, ErrConflict
	}
	event := repository.Audit{Action: "node.create", ResourceID: in.ID, RequestID: request, Reason: "create owned node", After: auditJSON(map[string]any{"owner_kind": in.OwnerKind, "lifecycle": "pending", "version": 1})}
	err = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &event, func(q *generated.Queries) error {
		if err := q.CreateOwnedResource(ctx, generated.CreateOwnedResourceParams{ResourceID: in.ID, ResourceKind: "node", OwnerKind: in.OwnerKind, OwnerTenantID: tenant}); err != nil {
			return err
		}
		var err error
		result, err = q.InsertNode(ctx, generated.InsertNodeParams{ID: in.ID, Name: in.Name, Region: in.Region, HubID: in.HubID, Capabilities: in.Capabilities, PublicAddress: in.PublicAddress, ProgramVersion: in.ProgramVersion, PortMin: in.PortMin, PortMax: in.PortMax, LinkPort: in.LinkPort, ControlPort: in.ControlPort, MetricsPort: in.MetricsPort, Capacity: in.Capacity})
		return err
	})
	if err != nil {
		return generated.Node{}, err
	}
	return result, nil
}
func (s NodeService) CreateGroup(ctx context.Context, scope TenantScope, in GroupInput, request string) (generated.NodeGroup, error) {
	var result generated.NodeGroup
	tenant, err := ownership(scope, in.OwnerKind)
	if err != nil {
		return result, err
	}
	if !validID(in.ID) {
		return result, ErrConflict
	}
	err = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "group.create", ResourceID: in.ID, RequestID: request, Reason: "create owned group"}, func(q *generated.Queries) error {
		if err := q.CreateOwnedResource(ctx, generated.CreateOwnedResourceParams{ResourceID: in.ID, ResourceKind: "node_group", OwnerKind: in.OwnerKind, OwnerTenantID: tenant}); err != nil {
			return err
		}
		var err error
		result, err = q.InsertNodeGroup(ctx, generated.InsertNodeGroupParams{ID: in.ID, Name: in.Name, Purpose: in.Purpose, Scheduling: in.Scheduling, HealthIntervalSeconds: in.HealthIntervalSeconds, HealthTimeoutSeconds: in.HealthTimeoutSeconds, BillingMultiplierMilli: in.BillingMultiplierMilli})
		return err
	})
	if err != nil {
		return generated.NodeGroup{}, err
	}
	return result, nil
}

// Replacement locks the group and every node in stable order. Any failed member
// rolls back the entire set; no member is silently filtered for availability.
func (s NodeService) SetMembers(ctx context.Context, scope TenantScope, group string, expected int64, members []GroupMember, request string) error {
	members = append([]GroupMember(nil), members...)
	sort.Slice(members, func(i, j int) bool { return members[i].NodeID < members[j].NodeID })
	if len(members) > 1024 {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "group.members", ResourceID: group, RequestID: request, Reason: "replace fixed member set"}, func(q *generated.Queries) error {
		g, err := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: group, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
		if err != nil {
			return err
		}
		if !owns(scope, g.OwnerKind, g.OwnerTenantID) {
			return ErrScope
		}
		if g.Version != expected {
			return ErrConflict
		}
		for _, m := range members {
			n, e := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: m.NodeID, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
			if e != nil {
				return e
			}
			if !owns(scope, n.OwnerKind, n.OwnerTenantID) {
				return ErrScope
			}
		}
		if err = q.ClearGroupMembers(ctx, group); err != nil {
			return err
		}
		for _, m := range members {
			if err = q.AddGroupMember(ctx, generated.AddGroupMemberParams{GroupID: group, NodeID: m.NodeID, Weight: m.Weight, Priority: m.Priority}); err != nil {
				return err
			}
		}
		next, err := q.AdvanceGroupMembers(ctx, generated.AdvanceGroupMembersParams{ID: group, Version: expected})
		if err != nil {
			return err
		}
		if err = q.MarkGroupPortRevalidation(ctx, group); err != nil {
			return err
		}
		if err = invalidateRules(ctx, q, scope, "group", group); err != nil {
			return err
		}
		return domainTask(ctx, q, scope, group, "group.revalidate_all_rules", "resource_change", next.Version)
	})
}
func snapshot(ctx context.Context, q *generated.Queries, scope TenantScope, group string) (MemberSnapshot, error) {
	g, err := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: group, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
	if err != nil {
		return MemberSnapshot{}, err
	}
	members, err := q.GroupMembers(ctx, group)
	if err != nil {
		return MemberSnapshot{}, err
	}
	// Include member incarnation/version/ranges as well as ordered identity. A
	// reboot or capability change cannot reuse a stale reservation snapshot.
	digest, err := repository.MemberSetDigest(g.MemberVersion, members)
	if err != nil {
		return MemberSnapshot{}, err
	}
	return MemberSnapshot{GroupID: group, GroupVersion: g.MemberVersion, MemberSetVersion: digest, Members: members}, nil
}
func (s NodeService) Snapshot(ctx context.Context, scope TenantScope, group, request string) (MemberSnapshot, error) {
	var result MemberSnapshot
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "group.snapshot", ResourceID: group, RequestID: request, Reason: "read authorized fixed member set"}, func(q *generated.Queries) error {
		var err error
		result, err = snapshot(ctx, q, scope, group)
		return err
	})
	if err != nil {
		return MemberSnapshot{}, err
	}
	return result, nil
}

func (s NodeService) Transition(ctx context.Context, scope TenantScope, node, state string, expected int64, drainSeconds int, request string) error {
	event := repository.Audit{Action: "node.lifecycle", ResourceID: node, RequestID: request, Reason: "node lifecycle transition"}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &event, func(q *generated.Queries) error {
		n, err := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: node, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
		if err != nil {
			return err
		}
		if !owns(scope, n.OwnerKind, n.OwnerTenantID) {
			return ErrScope
		}
		if n.Version != expected {
			return ErrConflict
		}
		event.Before = auditJSON(map[string]any{"lifecycle": n.Lifecycle, "version": n.Version})
		event.After = auditJSON(map[string]any{"lifecycle": state, "version": n.Version + 1})
		valid := map[string][]string{"pending": {"online", "offline", "disabled"}, "online": {"draining", "offline"}, "offline": {"online", "draining", "disabled"}, "draining": {"offline", "disabled"}, "disabled": {"pending"}}
		allowed := false
		for _, v := range valid[n.Lifecycle] {
			if v == state {
				allowed = true
			}
		}
		if !allowed {
			return ErrConflict
		}
		deadline := pgtype.Timestamptz{}
		if state == "draining" {
			if drainSeconds == 0 {
				drainSeconds = 300
			}
			if drainSeconds < 1 || drainSeconds > 86400 {
				return ErrConflict
			}
			deadline = timestamp(time.Now().Add(time.Duration(drainSeconds) * time.Second))
		}
		if state == "disabled" {
			occupied, e := q.CountNodePortOccupation(ctx, node)
			if e != nil {
				return e
			}
			if occupied > 0 {
				return ErrConflict
			}
			deps, e := q.NodeImpact(ctx, node)
			if e != nil {
				return e
			}
			for _, d := range deps {
				if d.Kind == "session" || d.Kind == "port" || d.Kind == "settlement" {
					return ErrConflict
				}
			}
		}
		next, err := q.SetNodeLifecycle(ctx, generated.SetNodeLifecycleParams{ID: node, Lifecycle: state, DrainDeadline: deadline, Version: expected})
		if err != nil {
			return err
		}
		return domainTask(ctx, q, scope, node, "node."+state, "resource_change", next.Version)
	})
}
func (s NodeService) Impact(ctx context.Context, scope TenantScope, node, request string) ([]generated.NodeDependency, error) {
	var result []generated.NodeDependency
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "node.impact", ResourceID: node, RequestID: request, Reason: "preview affected rules and sessions"}, func(q *generated.Queries) error {
		n, e := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: node, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
		if e != nil {
			return e
		}
		if !owns(scope, n.OwnerKind, n.OwnerTenantID) {
			return ErrScope
		}
		result, e = q.NodeImpact(ctx, node)
		return e
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s NodeService) DeleteNode(ctx context.Context, scope TenantScope, node string, expected int64, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.delete", ResourceID: node, RequestID: request, Reason: "soft delete after dependencies drained"}, func(q *generated.Queries) error {
		n, e := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: node, TenantID: scope.TenantID(), PlatformRead: scope.Platform()})
		if e != nil {
			return e
		}
		if !owns(scope, n.OwnerKind, n.OwnerTenantID) {
			return ErrScope
		}
		deps, e := q.CountNodeDependencies(ctx, node)
		if e != nil {
			return e
		}
		occupied, e := q.CountNodePortOccupation(ctx, node)
		if e != nil {
			return e
		}
		if occupied > 0 {
			return ErrConflict
		}
		if deps != 0 || n.Version != expected || n.Lifecycle != "disabled" {
			return ErrConflict
		}
		count, e := q.DeleteNode(ctx, generated.DeleteNodeParams{ID: node, Version: expected})
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrConflict
		}
		return domainTask(ctx, q, scope, node, "node.deleted", "resource_change", expected+1)
	})
}

func (s NodeService) SetGrant(ctx context.Context, scope TenantScope, resource string, expected int64, status string, from, until time.Time, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return ErrScope
	}
	if (status != "active" && status != "revoked") || expected < 0 || from.IsZero() || !until.After(from) {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "grant." + status, ResourceID: resource, RequestID: request, Reason: "versioned platform authorization"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		owner, e := q.LockResourceOwnership(ctx, resource)
		if e != nil {
			return e
		}
		if owner.OwnerKind != "platform" {
			return ErrScope
		}
		old, e := q.LatestGrant(ctx, generated.LatestGrantParams{TenantID: scope.TenantID(), ResourceID: resource})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if (errors.Is(e, pgx.ErrNoRows) && expected != 0) || (e == nil && old.Version != expected) {
			return ErrConflict
		}
		if e == nil {
			if e = q.RevokeResourceGrant(ctx, generated.RevokeResourceGrantParams{TenantID: scope.TenantID(), ResourceID: resource}); e != nil {
				return e
			}
			if e = q.AppendGrantHistory(ctx, generated.AppendGrantHistoryParams{TenantID: scope.TenantID(), ID: old.ID}); e != nil {
				return e
			}
		}
		grantID := uuid.NewString()
		e = q.InsertResourceGrant(ctx, generated.InsertResourceGrantParams{ID: grantID, TenantID: scope.TenantID(), ResourceID: resource, Version: expected + 1, Status: status, ValidFrom: timestamp(from), ValidUntil: timestamp(until)})
		if e != nil {
			return e
		}
		if e = q.AppendGrantHistory(ctx, generated.AppendGrantHistoryParams{TenantID: scope.TenantID(), ID: grantID}); e != nil {
			return e
		}
		if e = invalidateRules(ctx, q, scope, "grant", resource); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, resource, "grant.revalidate_rules", "resource_change", expected+1)
	})
}
func (s NodeService) SetPermit(ctx context.Context, scope TenantScope, from, to string, expected int64, allowed bool, request string) error {
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return ErrScope
	}
	if from == to || expected < 0 {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "segment.permit", ResourceID: from, RequestID: request, Reason: "explicit segment authorization"}, func(q *generated.Queries) error {
		ids := []string{from, to}
		sort.Strings(ids)
		for _, id := range ids {
			if _, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: id, TenantID: scope.TenantID(), PlatformRead: true}); e != nil {
				return e
			}
		}
		old, e := q.GetSegmentPermit(ctx, generated.GetSegmentPermitParams{TenantID: scope.TenantID(), FromGroupID: from, ToGroupID: to})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if (errors.Is(e, pgx.ErrNoRows) && expected != 0) || (e == nil && old.Version != expected) {
			return ErrConflict
		}
		permit, e := q.PutSegmentPermit(ctx, generated.PutSegmentPermitParams{TenantID: scope.TenantID(), FromGroupID: from, ToGroupID: to, Allowed: allowed, ExpectedVersion: expected})
		if e != nil {
			return e
		}
		if e = invalidateRules(ctx, q, scope, "permit", from+"/"+to); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, from, "segment.revalidate_rules", "resource_change", permit.Version)
	})
}
func (s NodeService) CheckSegment(ctx context.Context, scope TenantScope, from, to, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "segment.read", ResourceID: from, RequestID: request, Reason: "check independent segment authorization"}, func(q *generated.Queries) error {
		groups := map[string]generated.ScopedGroupRow{}
		ids := []string{from, to}
		sort.Strings(ids)
		for _, id := range ids {
			g, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: id, TenantID: scope.TenantID(), PlatformRead: false})
			if e != nil {
				return e
			}
			groups[id] = g
		}
		if from == to {
			return ErrConflict
		}
		if groups[from].OwnerKind == "tenant" && groups[to].OwnerKind == "tenant" {
			return nil
		}
		p, e := q.GetSegmentPermit(ctx, generated.GetSegmentPermitParams{TenantID: scope.TenantID(), FromGroupID: from, ToGroupID: to})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrScope
		}
		if e != nil {
			return e
		}
		if !p.Allowed {
			return ErrScope
		}
		return nil
	})
}
