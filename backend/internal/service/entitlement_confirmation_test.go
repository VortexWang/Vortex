package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type entitlementConfirmationAPI interface {
	PreviewEntitlement(context.Context, service.TenantScope, string, []string, time.Time) (json.RawMessage, error)
	ConfirmEntitlement(context.Context, service.TenantScope, string, []string, time.Time, string, string, string) (service.PackageResult, error)
}

func TestEntitlementDifferentPackageConfirmationAtomic(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, "INSERT INTO users(id,tenant_id,role,state) VALUES($1,$2,'owner','active')", f.scope.ActorID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	old := subscribePackage(t, f, createPackage(t, f, packageTerms()))
	target := createPackage(t, f, packageTerms())
	code := issueCode(t, f, target, "package", 12, 321, 1)
	s := service.PackageService{Pool: f.pool}
	api, ok := any(s).(entitlementConfirmationAPI)
	if !ok {
		t.Fatal("different-package entitlement preview/confirmation success path missing")
	}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "package_changes", "package_requests", "entitlement_redemptions", "audit_events", "operations", "tasks", "outbox"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	raw, e := api.PreviewEntitlement(ctx, f.scope, code, nil, at)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(code)) {
		t.Fatal("preview exposed code")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("preview wrote authority")
	}
	var p struct {
		Digest string `json:"digest"`
	}
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE users SET state='suspended' WHERE id=$1", f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if _, e = api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("revoked actor confirmed grant", e)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE users SET state='active' WHERE id=$1", f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	if _, e = api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, uuid.NewString(), request); e == nil {
		t.Fatal("audit failure accepted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial confirmation")
	}
	var uses int64
	if e = f.pool.QueryRow(ctx, "SELECT remaining_uses FROM entitlement_codes WHERE package_id=$1", target).Scan(&uses); e != nil || uses != 1 {
		t.Fatal("audit consumed code", uses, e)
	}
	other := issueCode(t, f, target, "package", 12, 321, 1)
	if _, e = api.ConfirmEntitlement(ctx, f.scope, other, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("digest not bound to code", e)
	}
	key := uuid.NewString()
	result, e := api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, key, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	replay, e := api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, key, uuid.NewString())
	if e != nil || replay != result {
		t.Fatal("exact replay", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, key, uuid.NewString())
			if e != nil || r != result {
				t.Error("concurrent exact replay", r, e)
			}
		}()
	}
	wg.Wait()
	if _, e = api.ConfirmEntitlement(ctx, f.scope, code, nil, at, p.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("exhausted code accepted on fresh key", e)
	}
	if _, e = api.ConfirmEntitlement(ctx, f.scope, other, nil, at, p.Digest, key, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("key conflict", e)
	}
	if e = f.pool.QueryRow(ctx, "SELECT remaining_uses FROM entitlement_codes WHERE package_id=$1 ORDER BY remaining_uses LIMIT 1", target).Scan(&uses); e != nil || uses != 0 {
		t.Fatal("code not consumed", uses, e)
	}
	sub, e := generated.New(f.pool).GetSubscription(ctx, f.tenant)
	if e != nil || sub.SnapshotID != old.SnapshotID {
		t.Fatal("activated early", e)
	}
	if worked, e := s.ProcessDuePackageChanges(ctx, at); e != nil || !worked {
		t.Fatal("due redemption", worked, e)
	}
	sub, terms, e := s.Subscription(ctx, f.scope, uuid.NewString())
	if e != nil || sub.SnapshotID != result.SnapshotID || terms.DurationDays != 12 || terms.TrafficBytes != 321 || !sub.ExpiresAt.Time.Equal(at.AddDate(0, 0, 12)) {
		t.Fatal("code terms not applied", sub, terms, e)
	}
	rows, e := generated.New(f.pool).ListTrafficSources(ctx, f.tenant)
	if e != nil || len(rows) != 2 {
		t.Fatal("code source amount", rows, e)
	}
	found := false
	for _, row := range rows {
		if row.SnapshotID == result.SnapshotID && row.InitialAmount == 321 {
			found = true
		}
	}
	if !found {
		t.Fatal("code source not bound to new snapshot", rows)
	}
}
