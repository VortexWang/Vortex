package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"net/netip"
	"sort"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type PortService struct {
	Pool   *pgxpool.Pool
	NodeCA *x509.Certificate
}
type PortRequest struct {
	RuleID, GroupID, MemberSetVersion, ListenAddress, Purpose string
	Port                                                      int32
	Protocols                                                 []string
}
type PortPreparation struct {
	generated.PortAllocation
	EntryAddressMayChange bool
	OperationID           string `json:"operation_id"`
}
type PortConflictError struct{ SuggestedPort int32 }

func (e *PortConflictError) Error() string       { return "requested port unavailable" }
func (e *PortConflictError) Is(other error) bool { return other == ErrConflict }

type PortProof struct {
	AllocationID, NodeID, BootID, TaskID, LeaseToken, Kind, Result string
	Revision, Generation                                           int64
}

// A tenant/platform capability alone cannot mint an Agent receipt. Only the
// dedicated TLS-1.3 node trust root, canonical node identity and persisted task
// lease/incarnation can authorize the proof mutation. Hub liveness is no proof.
func (s PortService) verifiedNode(ctx context.Context, node string) ([]byte, error) {
	if s.NodeCA == nil || !validID(node) {
		return nil, ErrScope
	}
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, ErrScope
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || info.State.Version != tls.VersionTLS13 || !info.State.HandshakeComplete || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return nil, ErrScope
	}
	cert := info.State.PeerCertificates[0]
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != node || cert.Subject.CommonName != node {
		return nil, ErrScope
	}
	roots := x509.NewCertPool()
	roots.AddCert(s.NodeCA)
	intermediates := x509.NewCertPool()
	for _, c := range info.State.PeerCertificates[1:] {
		intermediates.AddCert(c)
	}
	if _, e := cert.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: node, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); e != nil {
		return nil, ErrScope
	}
	sum := sha256.Sum256(cert.Raw)
	return sum[:], nil
}
func (s PortService) RegisterNodeSession(ctx context.Context, scope TenantScope, node, boot string, generation int64, request string) error {
	if _, e := s.verifiedNode(ctx, node); e != nil {
		return e
	}
	if !validID(boot) || generation < 1 {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.incarnation", ResourceID: node, RequestID: request, Reason: "authenticated node boot and generation"}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityWrite(ctx, node); e != nil {
			return e
		}
		if _, e := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: node, TenantID: scope.TenantID(), PlatformRead: scope.Platform()}); e != nil {
			return e
		}
		_, e := q.RegisterNodeIncarnation(ctx, generated.RegisterNodeIncarnationParams{ID: node, BootID: boot, Generation: generation})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrConflict
		}
		return e
	})
}

func activeTenant(ctx context.Context, q *generated.Queries, scope TenantScope) error {
	t, e := q.LockTenant(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	if t.AccountState != "active" {
		return ErrScope
	}
	rows, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(time.Now())})
	if e != nil {
		return e
	}
	if len(rows) != 0 {
		return ErrScope
	}
	return nil
}
func (s PortService) PreparePortReservation(ctx context.Context, scope TenantScope, in PortRequest, request string) (PortPreparation, error) {
	var result generated.PortAllocation
	var reallocated bool
	var operation string
	if !validID(in.RuleID) || !validID(in.GroupID) || in.Purpose != "service" || in.Port < 0 || in.Port > 65535 || len(in.Protocols) < 1 || len(in.Protocols) > 2 {
		return PortPreparation{}, ErrConflict
	}
	in.Protocols = append([]string(nil), in.Protocols...)
	sort.Strings(in.Protocols)
	for i, p := range in.Protocols {
		if (p != "tcp" && p != "udp") || (i > 0 && p == in.Protocols[i-1]) {
			return PortPreparation{}, ErrConflict
		}
	}
	if in.ListenAddress != "*" {
		ip, e := netip.ParseAddr(in.ListenAddress)
		if e != nil || ip.String() != in.ListenAddress {
			return PortPreparation{}, ErrConflict
		}
		in.ListenAddress = ip.Unmap().String()
	}
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port.prepare", ResourceID: in.RuleID, RequestID: request, Reason: "reserve whole fixed member set"}, func(q *generated.Queries) error {
		if e := activeRule(ctx, q, scope, in.RuleID); e != nil {
			return e
		}
		// Platform actions still need this tenant's grant; selected scope does not
		// grant arbitrary tenants use of a platform entry group.
		group, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: in.GroupID, TenantID: scope.TenantID(), PlatformRead: false})
		if e != nil {
			return e
		}
		if group.Purpose != "entry" {
			return ErrConflict
		}
		snap, e := snapshot(ctx, q, scope, in.GroupID)
		if e != nil {
			return e
		}
		if len(snap.Members) == 0 || snap.MemberSetVersion != in.MemberSetVersion {
			return ErrConflict
		}
		lower, upper := int32(1), int32(65535)
		blocked := map[int32]bool{}
		for _, n := range snap.Members {
			if n.Lifecycle != "online" || !validID(n.BootID) || n.PublicAddress == "" {
				return ErrConflict
			}
			if n.PortMin > lower {
				lower = n.PortMin
			}
			if n.PortMax < upper {
				upper = n.PortMax
			}
			blocked[n.LinkPort] = true
			blocked[n.ControlPort] = true
			blocked[n.MetricsPort] = true
			rows, e := q.ConflictingPorts(ctx, generated.ConflictingPortsParams{NodeID: n.ID, ListenAddress: in.ListenAddress})
			if e != nil {
				return e
			}
			for _, r := range rows {
				for _, p := range in.Protocols {
					if r.Protocol == p {
						blocked[r.Port] = true
					}
				}
			}
		}
		port := in.Port
		if port == 0 {
			for candidate := lower; candidate <= upper; candidate++ {
				if !blocked[candidate] {
					port = candidate
					break
				}
			}
		}
		if port < lower || port > upper || blocked[port] {
			for candidate := lower; candidate <= upper; candidate++ {
				if !blocked[candidate] {
					return &PortConflictError{SuggestedPort: candidate}
				}
			}
			return &PortConflictError{}
		}
		reallocated, e = q.RuleHasReleasedPort(ctx, generated.RuleHasReleasedPortParams{TenantID: scope.TenantID(), RuleID: in.RuleID})
		if e != nil {
			return e
		}
		result, e = q.InsertPortAllocation(ctx, generated.InsertPortAllocationParams{ID: uuid.NewString(), TenantID: scope.TenantID(), RuleID: in.RuleID, GroupID: in.GroupID, MemberSetVersion: in.MemberSetVersion, Port: port, ListenAddress: in.ListenAddress, Protocols: in.Protocols, Purpose: in.Purpose})
		if e != nil {
			var pgerr *pgconn.PgError
			if errors.As(e, &pgerr) && pgerr.Code == "23505" && pgerr.ConstraintName == "port_rule_active" {
				return ErrConflict
			}
			return e
		}
		for _, n := range snap.Members {
			task, e := newDomainTask(ctx, q, scope, n.ID, "port.prepare/"+result.ID, "port_prepare", n.Generation)
			if e != nil {
				return e
			}
			for _, protocol := range in.Protocols {
				if e = q.InsertPortReservation(ctx, generated.InsertPortReservationParams{TenantID: scope.TenantID(), AllocationID: result.ID, NodeID: n.ID, Protocol: protocol, Port: port, ListenAddress: in.ListenAddress, BootID: n.BootID, Generation: n.Generation, PrepareTaskID: task.ID}); e != nil {
					return e
				}
			}
		}
		op, e := newDomainTask(ctx, q, scope, result.ID, "port.prepare.requested", "resource_change", result.Version)
		if e != nil {
			return e
		}
		if e = q.InsertPortOperationBinding(ctx, generated.InsertPortOperationBindingParams{TenantID: scope.TenantID(), AllocationID: result.ID, OperationID: op.ID, Action: "prepare", AllocationVersion: result.Version, ActorID: scope.ActorID(), ExpectedReservations: int32(len(snap.Members) * len(in.Protocols))}); e != nil {
			return e
		}
		operation = op.ID
		return nil
	})
	if err != nil {
		return PortPreparation{}, err
	}
	return PortPreparation{PortAllocation: result, EntryAddressMayChange: reallocated, OperationID: operation}, nil
}

// Group-before-allocation locking is shared with membership changes. The
// physical node trigger serializes conflicts across different groups/tenants.
func lockAllocation(ctx context.Context, q *generated.Queries, scope TenantScope, id string) (generated.PortAllocation, error) {
	zero := generated.PortAllocation{}
	if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
		return zero, e
	}
	a, e := q.GetPortAllocation(ctx, generated.GetPortAllocationParams{TenantID: scope.TenantID(), ID: id})
	if e != nil {
		return zero, e
	}
	if _, e = q.LockPortGroup(ctx, a.GroupID); e != nil {
		return zero, e
	}
	return q.LockPortAllocation(ctx, generated.LockPortAllocationParams{TenantID: scope.TenantID(), ID: id})
}
func (s PortService) CommitPortReservation(ctx context.Context, scope TenantScope, id, request string) error {
	_, e := s.commitPort(ctx, scope, id, nil, request)
	return e
}

func (s PortService) CommitPortWithOperation(ctx context.Context, scope TenantScope, id string, expected int64, request string) (PortMutationResult, error) {
	if expected < 1 {
		return PortMutationResult{}, ErrPrecondition
	}
	return s.commitPort(ctx, scope, id, &expected, request)
}
func (s PortService) commitPort(ctx context.Context, scope TenantScope, id string, expected *int64, request string) (PortMutationResult, error) {
	var result PortMutationResult
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port.commit", ResourceID: id, RequestID: request, Reason: "all fixed members confirmed"}, func(q *generated.Queries) error {
		a, e := lockAllocation(ctx, q, scope, id)
		if e != nil {
			return e
		}
		if expected != nil && a.Version != *expected {
			return ErrPrecondition
		}
		if a.State == "committed" {
			if expected != nil {
				return ErrConflict
			}
			return nil
		}
		if a.State != "preparing" || a.NeedsRevalidation {
			return ErrConflict
		}
		if e = activeRule(ctx, q, scope, a.RuleID); e != nil {
			return e
		}
		if _, e = q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: a.GroupID, TenantID: scope.TenantID(), PlatformRead: false}); e != nil {
			return e
		}
		snap, e := snapshot(ctx, q, scope, a.GroupID)
		if e != nil {
			return e
		}
		if snap.MemberSetVersion != a.MemberSetVersion {
			return ErrConflict
		}
		rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: id})
		if e != nil {
			return e
		}
		if len(rows) != len(snap.Members)*len(a.Protocols) {
			return ErrConflict
		}
		for _, r := range rows {
			if !r.ConfigConfirmed || r.State != "confirmed" {
				return ErrConflict
			}
		}
		// Mutable confirmation flags are not a substitute for the immutable
		// authenticated receipt tied to each original Node/task incarnation.
		if e = confirmedPortProofs(ctx, q, scope, a, rows); e != nil {
			return e
		}
		next, e := q.CommitPortAllocation(ctx, generated.CommitPortAllocationParams{TenantID: scope.TenantID(), ID: id})
		if e != nil {
			return e
		}
		if expected != nil {
			count, e := portIntentInventory(ctx, q, scope, a, rows)
			if e != nil {
				return e
			}
			op, e := appendPortIntent(ctx, q, scope, next, "commit", count, true)
			if e != nil {
				return e
			}
			result = PortMutationResult{PortAllocation: next, OperationID: op, OperationState: "succeeded"}
		}
		return nil
	})
	if err != nil {
		return PortMutationResult{}, err
	}
	return result, nil
}
func (s PortService) AbortPortReservation(ctx context.Context, scope TenantScope, id, request string) error {
	return s.release(ctx, scope, id, "abort", request)
}
func (s PortService) RequestRelease(ctx context.Context, scope TenantScope, id, reason, request string) error {
	if reason != "delete" && reason != "explicit" && reason != "retention" {
		return ErrConflict
	}
	return s.release(ctx, scope, id, reason, request)
}
func (s PortService) release(ctx context.Context, scope TenantScope, id, reason, request string) error {
	_, e := s.releasePort(ctx, scope, id, reason, nil, request)
	return e
}
func (s PortService) ReleasePortWithOperation(ctx context.Context, scope TenantScope, id, action string, expected int64, request string) (PortMutationResult, error) {
	if expected < 1 {
		return PortMutationResult{}, ErrPrecondition
	}
	if action != "release" && action != "abort" {
		return PortMutationResult{}, ErrConflict
	}
	reason := "explicit"
	if action == "abort" {
		reason = "abort"
	}
	return s.releasePort(ctx, scope, id, reason, &expected, request)
}
func (s PortService) releasePort(ctx context.Context, scope TenantScope, id, reason string, expected *int64, request string) (PortMutationResult, error) {
	var result PortMutationResult
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port.release_requested", ResourceID: id, RequestID: request, Reason: "proof required: " + reason}, func(q *generated.Queries) error {
		a, e := lockAllocation(ctx, q, scope, id)
		if e != nil {
			return e
		}
		if expected != nil && a.Version != *expected {
			return ErrPrecondition
		}
		if a.State == "released" || a.State == "aborted" || a.State == "releasing" || a.State == "aborting" {
			if expected != nil {
				return ErrConflict
			}
			return nil
		}
		if reason == "abort" && a.State != "preparing" && a.State != "unknown" {
			return ErrConflict
		}
		if reason == "retention" && (a.State != "retained" || !a.RetainedUntil.Valid || time.Now().Before(a.RetainedUntil.Time)) {
			return ErrConflict
		}
		if reason == "retention" {
			active, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(time.Now())})
			if e != nil {
				return e
			}
			local, e := q.RuleHasPackageRestriction(ctx, generated.RuleHasPackageRestrictionParams{TenantID: scope.TenantID(), ResourceID: pgtype.Text{String: a.RuleID, Valid: true}, AtTime: timestamp(time.Now())})
			if e != nil {
				return e
			}
			if len(active) == 0 && !local {
				return ErrConflict
			}
			for _, r := range active {
				if r.Cause == "manual" {
					return ErrConflict
				}
			}
		}
		rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: id})
		if e != nil {
			return e
		}
		state := "releasing"
		if reason == "abort" {
			state = "aborting"
		}
		if e = q.SetPortAllocationState(ctx, generated.SetPortAllocationStateParams{TenantID: scope.TenantID(), ID: id, State: state}); e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, r := range rows {
			if seen[r.NodeID] || r.State == "released" {
				continue
			}
			seen[r.NodeID] = true
			task, e := newDomainTask(ctx, q, scope, r.NodeID, "port.release/"+id, "port_release", r.Generation)
			if e != nil {
				return e
			}
			if e = q.SetPortReleaseTask(ctx, generated.SetPortReleaseTaskParams{TenantID: scope.TenantID(), AllocationID: id, NodeID: r.NodeID, ReleaseTaskID: pgtype.Text{String: task.ID, Valid: true}}); e != nil {
				return e
			}
		}
		if expected != nil {
			count, e := portIntentInventory(ctx, q, scope, a, rows)
			if e != nil {
				return e
			}
			next, e := q.GetPortAllocation(ctx, generated.GetPortAllocationParams{TenantID: scope.TenantID(), ID: id})
			if e != nil {
				return e
			}
			action := "release"
			if reason == "abort" {
				action = "abort"
			}
			op, e := appendPortIntent(ctx, q, scope, next, action, count, false)
			if e != nil {
				return e
			}
			result = PortMutationResult{PortAllocation: next, OperationID: op, OperationState: "pending"}
		}
		return nil
	})
	if err != nil {
		return PortMutationResult{}, err
	}
	return result, nil
}

func (s PortService) MarkRestricted(ctx context.Context, scope TenantScope, id, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Restrict, &repository.Audit{Action: "port.restricted", ResourceID: id, RequestID: request, Reason: "retain from first effective business restriction"}, func(q *generated.Queries) error {
		a, e := lockAllocation(ctx, q, scope, id)
		if e != nil {
			return e
		}
		return restrictPortAllocation(ctx, q, scope, a)
	})
}

func restrictPortAllocation(ctx context.Context, q *generated.Queries, scope TenantScope, a generated.PortAllocation) error {
	id := a.ID
	if a.State != "committed" && a.State != "retained" && a.State != "preparing" && a.State != "unknown" {
		return ErrConflict
	}
	rows, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(time.Now())})
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		return ErrConflict
	}
	first := time.Time{}
	for _, r := range rows {
		if r.Cause != "manual" && (first.IsZero() || r.EffectiveAt.Time.Before(first)) {
			first = r.EffectiveAt.Time
		}
	}
	if first.IsZero() {
		e = q.SetPortAllocationState(ctx, generated.SetPortAllocationStateParams{TenantID: scope.TenantID(), ID: id, State: "retained"})
	} else {
		e = q.RetainPortAllocation(ctx, generated.RetainPortAllocationParams{TenantID: scope.TenantID(), ID: id, EffectiveAt: timestamp(first)})
	}
	if e != nil {
		return e
	}
	if e = q.SetReservationState(ctx, generated.SetReservationStateParams{TenantID: scope.TenantID(), AllocationID: id, State: "retained"}); e != nil {
		return e
	}
	return domainTask(ctx, q, scope, a.RuleID, "rule.stop_restricted", "resource_change", a.Version+1)
}

// These helpers execute inside ChangeRestriction's shared audit transaction.
func restrictTenantPorts(ctx context.Context, q *generated.Queries, scope TenantScope) error {
	rows, e := q.ListTenantPortAllocations(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	for _, a := range rows {
		if a.State == "releasing" || a.State == "aborting" {
			continue
		}
		locked, e := lockAllocation(ctx, q, scope, a.ID)
		if e != nil {
			return e
		}
		if e = restrictPortAllocation(ctx, q, scope, locked); e != nil {
			return e
		}
	}
	return nil
}
func clearTenantPortClocks(ctx context.Context, q *generated.Queries, scope TenantScope) error {
	active, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(time.Now())})
	if e != nil {
		return e
	}
	if len(active) > 0 {
		return nil
	}
	rows, e := q.ListTenantPortAllocations(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	for _, a := range rows {
		if a.State != "retained" {
			continue
		}
		local, e := q.RuleHasPackageRestriction(ctx, generated.RuleHasPackageRestrictionParams{TenantID: scope.TenantID(), ResourceID: pgtype.Text{String: a.RuleID, Valid: true}, AtTime: timestamp(time.Now())})
		if e != nil {
			return e
		}
		if local {
			continue
		}
		if _, e = lockAllocation(ctx, q, scope, a.ID); e != nil {
			return e
		}
		if e = q.ClearPortRetentionClock(ctx, generated.ClearPortRetentionClockParams{TenantID: scope.TenantID(), ID: a.ID}); e != nil {
			return e
		}
		if e = domainTask(ctx, q, scope, a.RuleID, "rule.resume_requires_prepare", "resource_change", a.Version+1); e != nil {
			return e
		}
	}
	return nil
}
func (s PortService) ResumeRetainedPort(ctx context.Context, scope TenantScope, id, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port.resume", ResourceID: id, RequestID: request, Reason: "all restrictions cleared; new restriction starts a new clock"}, func(q *generated.Queries) error {
		a, e := lockAllocation(ctx, q, scope, id)
		if e != nil {
			return e
		}
		if a.State != "retained" || a.NeedsRevalidation {
			return ErrConflict
		}
		if e = activeRule(ctx, q, scope, a.RuleID); e != nil {
			return e
		}
		snap, e := snapshot(ctx, q, scope, a.GroupID)
		if e != nil {
			return e
		}
		if snap.MemberSetVersion != a.MemberSetVersion {
			return ErrConflict
		}
		if e = q.ResetPortRetention(ctx, generated.ResetPortRetentionParams{TenantID: scope.TenantID(), ID: id}); e != nil {
			return e
		}
		rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: id})
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, r := range rows {
			if seen[r.NodeID] {
				continue
			}
			seen[r.NodeID] = true
			task, e := newDomainTask(ctx, q, scope, r.NodeID, "port.prepare/"+id, "port_prepare", r.Generation)
			if e != nil {
				return e
			}
			if e = q.RenewPortPrepareTask(ctx, generated.RenewPortPrepareTaskParams{TenantID: scope.TenantID(), AllocationID: id, NodeID: r.NodeID, PrepareTaskID: task.ID}); e != nil {
				return e
			}
		}
		return domainTask(ctx, q, scope, a.RuleID, "rule.resume_retained", "resource_change", a.Version+1)
	})
}

func (s PortService) RecordPrepareProof(ctx context.Context, scope TenantScope, p PortProof, request string) error {
	if p.Kind != "prepare" {
		return ErrStaleReceipt
	}
	return s.recordProof(ctx, scope, p, request)
}
func (s PortService) RecordReleaseProof(ctx context.Context, scope TenantScope, p PortProof, request string) error {
	if p.Kind != "release" {
		return ErrStaleReceipt
	}
	return s.recordProof(ctx, scope, p, request)
}

// ReleaseUnknown records only uncertainty. Unlike a success proof it conveys
// no authority to free sockets and therefore needs no node impersonation.
func (s PortService) ReleaseUnknown(ctx context.Context, scope TenantScope, p PortProof, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port.release_unknown", ResourceID: p.AllocationID, RequestID: request, Reason: "stop proof absent; retain physical occupation"}, func(q *generated.Queries) error {
		a, err := lockAllocation(ctx, q, scope, p.AllocationID)
		if err != nil {
			return err
		}
		if a.State == "released" || a.State == "aborted" {
			return nil
		}
		t, err := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: scope.TenantID(), ID: p.TaskID})
		if err != nil {
			return err
		}
		if t.TaskKind != "port_release" || !t.LeaseToken.Valid || t.LeaseToken.String != p.LeaseToken || (t.State != "running" && t.State != "leased") {
			return ErrStaleReceipt
		}
		if err = q.MarkNodePortUnknown(ctx, generated.MarkNodePortUnknownParams{TenantID: scope.TenantID(), AllocationID: p.AllocationID, NodeID: p.NodeID}); err != nil {
			return err
		}
		if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.TaskID, State: "unknown"}); err != nil {
			return err
		}
		return q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.TaskID, State: "unknown"})
	})
}
func (s PortService) recordProof(ctx context.Context, scope TenantScope, p PortProof, request string) error {
	fingerprint, e := s.verifiedNode(ctx, p.NodeID)
	if e != nil {
		return e
	}
	if !validID(p.BootID) || p.TaskID == "" || p.LeaseToken == "" || p.Revision < 1 || p.Generation < 1 {
		return ErrStaleReceipt
	}
	if p.Result != "unknown" && !(p.Kind == "prepare" && p.Result == "prepared") && !(p.Kind == "release" && p.Result == "released") {
		return ErrStaleReceipt
	}
	body, _ := json.Marshal(p)
	digest := sha256.Sum256(body)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "port." + p.Kind + "_proof", ResourceID: p.AllocationID, RequestID: request, Reason: "verified node incarnation task and revision receipt"}, func(q *generated.Queries) error {
		a, e := lockAllocation(ctx, q, scope, p.AllocationID)
		if e != nil {
			return e
		}
		prior, e := q.GetPortProof(ctx, generated.GetPortProofParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: p.NodeID, TaskID: p.TaskID})
		if e == nil {
			if bytes.Equal(prior.ReceiptSha256, digest[:]) && bytes.Equal(prior.PeerFingerprint, fingerprint) {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		rows, e := q.PortReservations(ctx, generated.PortReservationsParams{TenantID: scope.TenantID(), AllocationID: a.ID})
		if e != nil {
			return e
		}
		matching := false
		for _, r := range rows {
			if r.NodeID != p.NodeID {
				continue
			}
			task := r.PrepareTaskID
			if p.Kind == "release" {
				task = r.ReleaseTaskID.String
			}
			if task != p.TaskID || r.BootID != p.BootID || r.Generation != p.Generation {
				return ErrStaleReceipt
			}
			matching = true
		}
		if !matching {
			return ErrStaleReceipt
		}
		n, e := q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: p.NodeID, TenantID: scope.TenantID(), PlatformRead: true})
		if e != nil {
			return e
		}
		if n.BootID != p.BootID || n.Generation != p.Generation {
			return ErrStaleReceipt
		}
		task, e := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: scope.TenantID(), ID: p.TaskID})
		if e != nil {
			return e
		}
		if task.TaskKind != "port_"+p.Kind || task.TargetRevision != p.Revision || task.Generation != p.Generation || task.LeaseToken.String != p.LeaseToken || !task.LeaseToken.Valid || (task.State != "leased" && task.State != "running" && task.State != "unknown") {
			return ErrStaleReceipt
		}
		if (p.Kind == "prepare" && a.State != "preparing") || (p.Kind == "release" && a.State != "releasing" && a.State != "aborting") {
			return ErrStaleReceipt
		}
		if p.Result == "unknown" {
			if e = q.MarkNodePortUnknown(ctx, generated.MarkNodePortUnknownParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: p.NodeID}); e != nil {
				return e
			}
			if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.TaskID, State: "unknown"}); e != nil {
				return e
			}
			return q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.TaskID, State: "unknown"})
		}
		if e = q.InsertPortProof(ctx, generated.InsertPortProofParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: p.NodeID, TaskID: p.TaskID, BootID: p.BootID, Revision: p.Revision, Generation: p.Generation, ProofKind: p.Kind, ReceiptSha256: digest[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		if p.Kind == "prepare" {
			e = q.ConfirmNodePortPrepare(ctx, generated.ConfirmNodePortPrepareParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: p.NodeID})
		} else {
			e = q.ConfirmNodePortRelease(ctx, generated.ConfirmNodePortReleaseParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: p.NodeID})
		}
		if e != nil {
			return e
		}
		if e = q.FinishPortTask(ctx, generated.FinishPortTaskParams{TenantID: scope.TenantID(), ID: p.TaskID}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.TaskID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: p.TaskID}); e != nil {
			return e
		}
		if p.Kind == "release" {
			all := true
			for _, r := range rows {
				if r.NodeID != p.NodeID && r.State != "released" {
					all = false
				}
			}
			if all {
				state := "released"
				if a.State == "aborting" {
					state = "aborted"
				}
				return q.SetPortAllocationState(ctx, generated.SetPortAllocationStateParams{TenantID: scope.TenantID(), ID: a.ID, State: state})
			}
		}
		return nil
	})
}
