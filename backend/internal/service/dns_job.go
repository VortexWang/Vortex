package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"

	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// ProcessDNSJob reconstructs only a currently authorized persisted identity
// from an immutable internal intent; caller-selected actor IDs grant nothing.
// A provider error is a completed unknown projection, parked until a new
// recompute request. Lease/audit failures remain errors and retain uncertainty.
func (s ServingService) ProcessDNSJob(ctx context.Context, tenant, id, request string, store DNSCredentialAccess) (bool, error) {
	if !validID(tenant) || !validID(id) || request == "" || len(request) > 100 {
		return false, ErrConflict
	}
	q := generated.New(s.Pool)
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: tenant, ID: id})
	if e != nil {
		return false, e
	}
	var job DNSDesired
	digest := sha256.Sum256(op.Payload)
	if json.Unmarshal(op.Payload, &job) != nil || job.Action != "dns.reconcile" || job.Schema != 1 || !validID(job.ActorID) || !validID(job.GroupID) || !validID(job.AccessID) || !ValidEntryDomain(job.Name) || !bytes.Equal(digest[:], op.RequestSha256) {
		return false, ErrConflict
	}
	if op.State == "succeeded" {
		return false, nil
	}
	if op.State != "pending" && op.State != "leased" {
		return false, ErrConflict
	}
	home := tenant
	if job.PlatformActor {
		home = ""
	}
	p, e := (repository.TokenRepository{Pool: s.Pool}).LocalSessionPrincipal(ctx, job.ActorID, home)
	if e != nil {
		return false, ErrScope
	}
	scope, e := repository.NewTenantScope(p, tenant, policy.Mutate)
	if e != nil {
		return false, e
	}
	e = s.ApplyDNS(ctx, scope, id, request, store)
	if e != nil {
		current, readErr := q.GetOperation(ctx, generated.GetOperationParams{TenantID: tenant, ID: id})
		if readErr == nil && current.State == "unknown" {
			return true, nil
		}
		return false, e
	}
	return true, nil
}
