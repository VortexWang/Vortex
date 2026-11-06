package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestTenantRestrictionSchema(t *testing.T) {
 pool := testutil.Postgres(t)
 for _, table := range []string{"tenants", "users", "platform_principals", "resource_grants", "restriction_events", "current_restrictions"} {
  var exists bool
  if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil { t.Fatal(err) }
  if !exists { t.Errorf("tenant policy authority table %s is missing", table) }
 }
}
