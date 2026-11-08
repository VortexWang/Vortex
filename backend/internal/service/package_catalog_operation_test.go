package service_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestPackageCatalogOperationAdminScopeRollbackAndCrossTenantCreationCAS(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	s := service.PackageService{Pool: pool}
	id := uuid.NewString()
	operator := policyScope(t, tenant, auth.Operator, policy.Mutate)
	if _, e := s.PutCatalog(ctx, operator, id, "forbidden", 0, packageTerms(), uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("operator catalog mutation", e)
	}
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	request := uuid.NewString()
	failAudit(t, pool, request)
	r, e := s.PutCatalogWithOperation(ctx, admin, id, "rollback", 0, packageTerms(), request)
	if e == nil || r.OperationID != "" || r.PackageID != "" || r.Version != 0 {
		t.Fatal("failed audit returned accepted result", r, e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM package_catalog WHERE id=$1`, id).Scan(&count); e != nil || count != 0 {
		t.Fatal("catalog escaped audit failure", count, e)
	}
	scopes := []service.TenantScope{admin}
	for i := 1; i < 8; i++ {
		scopes = append(scopes, policyScope(t, seedTenant(t, pool), auth.PlatformAdmin, policy.Mutate))
	}
	type outcome struct {
		result service.CatalogMutationResult
		err    error
	}
	ch := make(chan outcome, 8)
	var wg sync.WaitGroup
	for _, scope := range scopes {
		wg.Add(1)
		go func(scope service.TenantScope) {
			defer wg.Done()
			r, e := s.PutCatalogWithOperation(ctx, scope, id, "one global catalog", 0, packageTerms(), uuid.NewString())
			ch <- outcome{r, e}
		}(scope)
	}
	wg.Wait()
	close(ch)
	accepted, stale := 0, 0
	for o := range ch {
		if o.err == nil {
			accepted++
			if o.result.OperationID == "" || o.result.Version != 1 || o.result.PackageID != id {
				t.Fatal(o)
			}
		} else if errors.Is(o.err, service.ErrPrecondition) && errors.Is(o.err, service.ErrConflict) {
			stale++
		} else {
			t.Fatal(o)
		}
	}
	if accepted != 1 || stale != 7 {
		t.Fatal(accepted, stale)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM package_versions WHERE package_id=$1`, id).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
