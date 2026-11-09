package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"sort"
	"strings"
	"time"
	"vortex/backend/db/generated"

	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type DNSCredentialAccess interface {
	Reconcile(context.Context, repository.TenantScope, string, string, string, []string, string, string) error
}

type ServingService struct{ Pool *pgxpool.Pool }

var entryDomainName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func ValidEntryDomain(name string) bool {
	return len(name) <= 253 && entryDomainName.MatchString(name) && name == strings.ToLower(name)
}

type DNSDesired struct {
	ActorID       string   `json:"actor_id,omitempty"`
	PlatformActor bool     `json:"platform_actor,omitempty"`
	Schema        int      `json:"dns_schema,omitempty"`
	Action        string   `json:"action"`
	GroupID       string   `json:"group_id"`
	AccessID      string   `json:"access_id"`
	ZoneID        string   `json:"zone_id"`
	Name          string   `json:"name"`
	Addresses     []string `json:"addresses"`
}

// RecomputeDNS takes the membership and confirmed ports from one transaction,
// then commits the safe desired DNS/task/outbox/audit as a single operation.
// Queueing never performs external I/O or treats DNS as a port release proof.
func (s ServingService) RecomputeDNS(ctx context.Context, scope TenantScope, group, access, name, key, request string) (generated.Operation, error) {
	var op generated.Operation
	if len(key) < 1 || len(key) > 128 || !ValidEntryDomain(name) {
		return op, ErrConflict
	}
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "dns.recompute", ResourceID: group, RequestID: request, Reason: "confirmed current members only; TTL 60 DNS-only"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		if e := servingCurrentRole(ctx, q, scope); e != nil {
			return e
		}
		g, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{TenantID: scope.TenantID(), GroupID: group, PlatformRead: scope.Platform()})
		if e != nil {
			return e
		}
		if !owns(scope, g.OwnerKind, g.OwnerTenantID) || g.Purpose != "entry" || g.Scheduling != "dns" {
			return ErrConflict
		}
		credential, e := q.GetEncryptedCredential(ctx, generated.GetEncryptedCredentialParams{TenantID: scope.TenantID(), ID: access})
		if e != nil {
			return e
		}
		if credential.ZoneName == "" || (name != credential.ZoneName && !strings.HasSuffix(name, "."+credential.ZoneName)) {
			return ErrConflict
		}
		if _, e = q.BindEntryGroupDomain(ctx, generated.BindEntryGroupDomainParams{TenantID: scope.TenantID(), ID: group, CredentialID: access, ZoneID: credential.ZoneID, Name: name}); e != nil {
			return ErrConflict
		}
		// A retry refers to the already accepted snapshot, even if serving has
		// since changed. Current role/ownership/zone binding are still checked.
		prior, e := q.GetOperationByKey(ctx, generated.GetOperationByKeyParams{TenantID: scope.TenantID(), IdempotencyKey: "dns/" + key})
		if e == nil {
			var desired DNSDesired
			digest := sha256.Sum256(prior.Payload)
			if json.Unmarshal(prior.Payload, &desired) != nil || desired.Action != "dns.reconcile" || desired.GroupID != group || desired.AccessID != access || desired.Name != name || desired.ZoneID != credential.ZoneID || !bytes.Equal(digest[:], prior.RequestSha256) {
				return repository.ErrDNSOperation
			}
			op = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		addresses := []string{}
		if e = activeTenant(ctx, q, scope); e == nil || scope.Platform() {
			rows, e := repository.ServingMembers(ctx, q, scope, group)
			if e != nil {
				return e
			}
			set := map[string]bool{}
			for _, row := range rows {
				set[row.PublicAddress] = true
			}
			for address := range set {
				addresses = append(addresses, address)
			}
			sort.Strings(addresses)
		} else if !errors.Is(e, ErrScope) {
			return e
		}
		op, e = repository.EnqueueDNS(ctx, q, scope, key, DNSDesired{ActorID: scope.ActorID(), PlatformActor: scope.Platform(), Schema: 1, Action: "dns.reconcile", GroupID: group, AccessID: access, ZoneID: credential.ZoneID, Name: name, Addresses: addresses})
		return e
	})
	if err != nil {
		return generated.Operation{}, err
	}
	return op, nil
}
func servingCurrentRole(ctx context.Context, q *generated.Queries, s TenantScope) error {
	if s.Platform() {
		p, e := q.GetPlatformPrincipal(ctx, s.ActorID())
		if e != nil || p.State != "active" || (p.Role != "platform_admin" && p.Role != "operator") {
			return ErrScope
		}
		return nil
	}
	p, e := q.GetTenantUser(ctx, generated.GetTenantUserParams{TenantID: s.TenantID(), ID: s.ActorID()})
	if e != nil || p.State != "active" || (p.Role != "owner" && p.Role != "member") {
		return ErrScope
	}
	return nil
}

// ApplyDNS leases a persisted operation, performs bounded network I/O after the
// transaction, then records success or unknown. A lost response is reconciled by
// provider name/ownership lookup, never a blind POST retry.
func (s ServingService) ApplyDNS(ctx context.Context, scope TenantScope, id, request string, store DNSCredentialAccess) error {
	var desired DNSDesired
	lease := uuid.NewString()
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "dns.lease", ResourceID: id, RequestID: request, Reason: "lease persisted DNS desired state"}, func(q *generated.Queries) error {
		if e := servingCurrentRole(ctx, q, scope); e != nil {
			return e
		}
		op, e := q.LockOperation(ctx, generated.LockOperationParams{TenantID: scope.TenantID(), ID: id})
		if e != nil {
			return e
		}
		if op.State == "succeeded" {
			return nil
		}
		if json.Unmarshal(op.Payload, &desired) != nil || desired.Action != "dns.reconcile" {
			return ErrConflict
		}
		binding, e := q.GetEntryGroupDomain(ctx, generated.GetEntryGroupDomainParams{TenantID: scope.TenantID(), GroupID: desired.GroupID})
		if e != nil || binding.Name != desired.Name || binding.CredentialID != desired.AccessID || binding.ZoneID != desired.ZoneID {
			return ErrConflict
		}
		n, e := q.LeaseDNSOperation(ctx, generated.LeaseDNSOperationParams{TenantID: scope.TenantID(), OperationID: id, LeaseToken: pgtype.Text{String: lease, Valid: true}})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		return q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: id, State: "leased"})
	})
	if err != nil {
		return err
	}
	if desired.Action == "" {
		return nil
	}
	// Re-read authoritative serving immediately before external effects. If the
	// persisted set no longer matches, mark unknown and require recomputation.
	rows, e := (repository.PortRepository{Pool: s.Pool}).Serving(ctx, scope, desired.GroupID, request+"/check")
	actual := map[string]bool{}
	for _, r := range rows {
		actual[r.PublicAddress] = true
	}
	same := e == nil && len(actual) == len(desired.Addresses)
	for _, address := range desired.Addresses {
		if !actual[address] {
			same = false
		}
	}
	if !same {
		err = ErrConflict
	} else if store == nil {
		err = ErrConflict
	} else {
		ioCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = store.Reconcile(ioCtx, scope, desired.AccessID, desired.ZoneID, desired.Name, desired.Addresses, scope.TenantID()+":"+desired.GroupID, request+"/access")
		cancel()
	}
	state := "succeeded"
	if err != nil {
		state = "unknown"
	}
	finish := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "dns.result", ResourceID: id, RequestID: request + "/result", Reason: "provider result; unknown preserves occupation"}, func(q *generated.Queries) error {
		n, e := q.FinishDNSOperation(ctx, generated.FinishDNSOperationParams{TenantID: scope.TenantID(), OperationID: id, LeaseToken: pgtype.Text{String: lease, Valid: true}, State: state})
		if e != nil {
			return e
		}
		if n != 1 {
			return pgx.ErrNoRows
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: id, State: state}); e != nil {
			return e
		}
		if state == "succeeded" {
			return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: id})
		}
		return nil
	})
	if finish != nil {
		return finish
	}
	return err
}
