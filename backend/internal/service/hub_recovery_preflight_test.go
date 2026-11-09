package service_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func TestHubRecoveryPreflightAuthenticatedApprovalScopeAndAuditRollback(t *testing.T) {
	x, in, _, _, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	p, e := x.s.PrepareHubSameStoreRecovery(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = x.s.HubRecoveryPreflight(context.Background(), r.HubID, r.ToBootID, r.BackendID, uuid.NewString()); e == nil {
		t.Fatal("unauthenticated preflight")
	}
	if _, _, e = x.s.HubRecoveryPreflight(ctx, r.HubID, r.ToBootID, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("wrong backend", e)
	}
	if _, _, e = x.s.HubRecoveryPreflight(ctx, r.HubID, in.HubBootID, r.BackendID, uuid.NewString()); e == nil {
		t.Fatal("old boot bypassed next approval")
	}
	tables := []string{"operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, _, e = x.s.HubRecoveryPreflight(ctx, r.HubID, r.ToBootID, r.BackendID, request); e == nil {
		t.Fatal("preflight escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("failed preflight committed")
	}
	approved, available, e := x.s.HubRecoveryPreflight(ctx, r.HubID, r.ToBootID, r.BackendID, uuid.NewString())
	if e != nil || available || !bytes.Equal(approved.Payload, p.Payload) {
		t.Fatal(available, e)
	}
}
