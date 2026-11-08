package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
	"vortex/backend/internal/service"
)

type versionedPackageConfirmationAPI interface {
	ConfirmDowngradeAtVersion(context.Context, service.TenantScope, string, int64, int64, []string, time.Time, string, string, string) (service.PackageResult, error)
	ConfirmEntitlementAtVersion(context.Context, service.TenantScope, string, int64, []string, time.Time, string, string, string) (service.PackageResult, error)
}

func TestPackageConfirmationVersionCASIsTransactionalAndReplayKeepsOriginalIdentity(t *testing.T) {
	ctx := context.Background()
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	if _, e := f.pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner')`, f.scope.ActorID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	s := service.PackageService{Pool: f.pool}
	api, ok := any(s).(versionedPackageConfirmationAPI)
	if !ok {
		t.Fatal("transaction-bound versioned package confirmation adapter missing")
	}
	original := createPackage(t, f, packageTerms())
	sub := subscribePackage(t, f, original)
	target := createPackage(t, f, packageTerms())
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	raw, e := s.PreviewDowngrade(ctx, f.scope, target, 1, nil, at)
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Digest string `json:"digest"`
	}
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "package_changes", "package_requests", "audit_events", "operations", "tasks", "outbox"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 2, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrPrecondition) || !errors.Is(e, service.ErrConflict) {
		t.Fatal("stale If-Match was not independently enforced", e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("precondition changed authority")
	}
	key := uuid.NewString()
	result, e := api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 1, nil, at, p.Digest, key, uuid.NewString())
	if e != nil || result.OperationID == "" || result.SubscriptionID != sub.SubscriptionID {
		t.Fatal(result, e)
	}
	if _, e = api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 1, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrConflict) || errors.Is(e, service.ErrPrecondition) {
		t.Fatal("another pending change is a state conflict", e)
	}
	duration := issueCode(t, f, original, "duration", 2, 0, 1)
	if advanced, e := s.RedeemEntitlement(ctx, f.scope, duration, uuid.NewString(), uuid.NewString()); e != nil || advanced.Version != 2 {
		t.Fatal("actual version advance", advanced, e)
	}
	replay, e := api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 1, nil, at, p.Digest, key, uuid.NewString())
	if e != nil || replay != result {
		t.Fatal("exact replay depends on changed current version", replay, e)
	}
	if _, e = api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 2, nil, at, p.Digest, key, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("idempotency key did not bind expected version", e)
	}
	if _, e = api.ConfirmDowngradeAtVersion(ctx, f.scope, target, 1, 1, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrPrecondition) {
		t.Fatal(e)
	}
	// Let the real due worker reject the now-stale original pending change in
	// this isolated schema, so it cannot preempt the later audit fault.
	if processed, e := s.ProcessDuePackageChanges(ctx, at.Add(time.Second)); e != nil || !processed {
		t.Fatal(processed, e)
	}
	code := issueCode(t, f, target, "package", 12, 321, 1)
	raw, e = s.PreviewEntitlement(ctx, f.scope, code, nil, at)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = api.ConfirmEntitlementAtVersion(ctx, f.scope, code, 1, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrPrecondition) {
		t.Fatal("code bypassed version guard", e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	if r, e := api.ConfirmEntitlementAtVersion(ctx, f.scope, code, 2, nil, at, p.Digest, uuid.NewString(), request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || r.OperationID != "" {
		t.Fatal("code audit failure accepted", r, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("stale/audit code confirmation partially changed authority")
	}
	var uses int
	if e = f.pool.QueryRow(ctx, `SELECT remaining_uses FROM entitlement_codes WHERE package_id=$1`, target).Scan(&uses); e != nil || uses != 1 {
		t.Fatal("failed version/audit consumed code", uses, e)
	}
}
