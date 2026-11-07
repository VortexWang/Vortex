package service

import (
	"context"
	"encoding/json"

	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
)

func validateRulePortMembers(ctx context.Context, q *generated.Queries, scope TenantScope, in compiler.CompileInput) error {
	if in.Rule.Enabled {
		if e := activeRule(ctx, q, scope, in.Rule.ID); e != nil {
			return e
		}
	}
	p := in.PortBinding
	if p == nil {
		return ErrConflict
	}
	a, e := lockAllocation(ctx, q, scope, p.ID)
	if e != nil {
		return e
	}
	if a.RuleID != in.Rule.ID || a.GroupID != p.GroupID || a.MemberSetVersion != p.MemberSetVersion || a.Port != p.Port || a.ListenAddress != p.ListenAddress || a.NeedsRevalidation || (a.State != "preparing" && a.State != "committed" && (in.Rule.Enabled || a.State != "retained")) {
		return ErrConflict
	}
	if len(a.Protocols) != len(p.Protocols) {
		return ErrConflict
	}
	protocols := map[string]bool{}
	for _, protocol := range a.Protocols {
		protocols[protocol] = true
	}
	for _, protocol := range p.Protocols {
		if !protocols[protocol] {
			return ErrConflict
		}
	}
	var entry compiler.Group
	for _, g := range in.Groups {
		if g.ID == p.GroupID {
			entry = g
		}
	}
	snap, e := snapshot(ctx, q, scope, p.GroupID)
	if e != nil {
		return e
	}
	if snap.MemberSetVersion != p.MemberSetVersion || len(entry.Members) != len(snap.Members) {
		return ErrConflict
	}
	members := map[string]compiler.Member{}
	for _, n := range entry.Members {
		members[n.ID] = n
	}
	rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: p.ID})
	if e != nil {
		return e
	}
	if len(rows) != len(entry.Members)*len(p.Protocols) {
		return ErrConflict
	}
	for _, row := range rows {
		n, ok := members[row.NodeID]
		if !ok || row.BootID != n.BootID || row.Generation != n.Generation || !protocols[row.Protocol] || row.Port != p.Port || row.ListenAddress != p.ListenAddress || row.State == "released" || row.State == "releasing" || (row.State == "retained" && in.Rule.Enabled) {
			return ErrConflict
		}
	}
	return nil
}

func deploymentPortInput(ctx context.Context, q *generated.Queries, scope TenantScope, d generated.RuleDeployment) (compiler.CompileInput, error) {
	p, e := q.GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: scope.TenantID(), RuleID: d.RuleID, RuleRevision: d.RuleRevision, InputDigest: d.InputDigest})
	var in compiler.CompileInput
	if e == nil {
		e = json.Unmarshal(p.Canonical, &in)
	}
	return in, e
}

// Transfer an unsent legacy reservation or a fully active previous deployment.
// Any unobserved execution must converge before a new owner takes over.
func bindRulePortPrepare(ctx context.Context, q *generated.Queries, scope TenantScope, d generated.RuleDeployment) error {
	in, e := deploymentPortInput(ctx, q, scope, d)
	if e != nil {
		return e
	}
	if e = validateRulePortMembers(ctx, q, scope, in); e != nil {
		return e
	}
	rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: in.PortBinding.ID})
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.NodeID] {
			continue
		}
		seen[row.NodeID] = true
		task, e := q.RuleMemberPrepareTask(ctx, generated.RuleMemberPrepareTaskParams{TenantID: scope.TenantID(), DeploymentID: d.ID, NodeID: row.NodeID})
		if e != nil {
			return e
		}
		if row.PrepareTaskID == task {
			continue
		}
		old, e := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: scope.TenantID(), ID: row.PrepareTaskID})
		if e != nil {
			return e
		}
		if old.TaskKind == "rule_prepare" && old.State == "succeeded" {
			b, err := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: scope.TenantID(), TaskID: old.ID})
			if err != nil {
				return err
			}
			previous, err := q.LockRuleDeployment(ctx, generated.LockRuleDeploymentParams{TenantID: scope.TenantID(), ID: b.DeploymentID})
			if err != nil {
				return err
			}
			members, err := q.RuleDeploymentMembers(ctx, generated.RuleDeploymentMembersParams{TenantID: scope.TenantID(), DeploymentID: b.DeploymentID})
			if err != nil {
				return err
			}
			if previous.RuleID != d.RuleID || previous.State != "running" || b.BootID != row.BootID {
				return ErrConflict
			}
			for _, m := range members {
				if m.State != "active" {
					return ErrConflict
				}
			}
			// The new immutable artifact must advance the runtime fence. The
			// reservation remains occupied throughout the transfer.
			next, err := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
			if err != nil {
				return err
			}
			if next.FencingGeneration <= b.FencingGeneration {
				return ErrConflict
			}
		} else {
			if old.TaskKind != "port_prepare" || old.State != "pending" {
				return ErrConflict
			}
			n, e := q.SupersedeUnsentPortTask(ctx, generated.SupersedeUnsentPortTaskParams{TenantID: scope.TenantID(), ID: old.ID})
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrConflict
			}
			if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: old.OperationID, State: "superseded"}); e != nil {
				return e
			}
			if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: old.OperationID}); e != nil {
				return e
			}
		}
		if e = q.BindRulePortPrepare(ctx, generated.BindRulePortPrepareParams{TenantID: scope.TenantID(), AllocationID: in.PortBinding.ID, NodeID: row.NodeID, PrepareTaskID: task}); e != nil {
			return e
		}
	}
	if !in.Rule.Enabled {
		return nil
	}
	return q.SetPortAllocationState(ctx, generated.SetPortAllocationStateParams{TenantID: scope.TenantID(), ID: in.PortBinding.ID, State: "preparing"})
}

func recordRulePortReceipt(ctx context.Context, q *generated.Queries, scope TenantScope, d generated.RuleDeployment, b generated.RuleDeliveryRow, task, lease, state string) error {
	in, e := deploymentPortInput(ctx, q, scope, d)
	if e != nil || in.PortBinding == nil {
		return e
	}
	if e = validateRulePortMembers(ctx, q, scope, in); e != nil {
		return e
	}
	entryMember := false
	for _, g := range in.Groups {
		if g.ID == in.PortBinding.GroupID {
			for _, n := range g.Members {
				if n.ID == b.NodeID {
					entryMember = true
				}
			}
		}
	}
	if !entryMember {
		return nil
	}
	leased, e := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: scope.TenantID(), ID: task})
	if e != nil {
		return e
	}
	if lease == "" || !leased.LeaseToken.Valid || leased.LeaseToken.String != lease || (leased.State != "running" && leased.State != "leased" && leased.State != "unknown") {
		return ErrStaleReceipt
	}
	node, e := q.ScopedNode(ctx, generated.ScopedNodeParams{TenantID: scope.TenantID(), NodeID: b.NodeID, PlatformRead: true})
	if e != nil {
		return e
	}
	if node.BootID != b.BootID {
		return ErrStaleReceipt
	}
	prepare, e := q.RuleMemberPrepareTask(ctx, generated.RuleMemberPrepareTaskParams{TenantID: scope.TenantID(), DeploymentID: d.ID, NodeID: b.NodeID})
	if e != nil {
		return e
	}
	rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: in.PortBinding.ID})
	if e != nil {
		return e
	}
	for _, row := range rows {
		if row.NodeID == b.NodeID && (row.PrepareTaskID != prepare || row.Generation != node.Generation || row.BootID != b.BootID) {
			return ErrStaleReceipt
		}
	}
	p := in.PortBinding.ID
	if state == "active" && !in.Rule.Enabled {
		// An active disabled configuration has no service socket. Keep physical
		// occupation until the dedicated release protocol proves it is reusable.
		return q.MarkNodePortUnknown(ctx, generated.MarkNodePortUnknownParams{TenantID: scope.TenantID(), AllocationID: p, NodeID: b.NodeID})
	}
	switch state {
	case "active":
		if e = q.ConfirmNodePortPrepare(ctx, generated.ConfirmNodePortPrepareParams{TenantID: scope.TenantID(), AllocationID: p, NodeID: b.NodeID}); e != nil {
			return e
		}
		all := true
		for _, row := range rows {
			if row.NodeID != b.NodeID && (!row.ConfigConfirmed || row.State != "confirmed") {
				all = false
			}
		}
		if all {
			if e = activeRule(ctx, q, scope, in.Rule.ID); e != nil {
				return e
			}
			a, e := q.GetPortAllocation(ctx, generated.GetPortAllocationParams{TenantID: scope.TenantID(), ID: p})
			if e != nil {
				return e
			}
			if a.State == "preparing" {
				_, e = q.CommitPortAllocation(ctx, generated.CommitPortAllocationParams{TenantID: scope.TenantID(), ID: p})
			}
			return e
		}
	case "unknown", "failed":
		return q.MarkNodePortUnknown(ctx, generated.MarkNodePortUnknownParams{TenantID: scope.TenantID(), AllocationID: p, NodeID: b.NodeID})
	case "aborted":
		// Release remains the dedicated port proof protocol. A rule transaction
		// abort cannot prove that another/historical transaction owns no socket.
		return q.MarkNodePortUnknown(ctx, generated.MarkNodePortUnknownParams{TenantID: scope.TenantID(), AllocationID: p, NodeID: b.NodeID})
	}
	return nil
}
