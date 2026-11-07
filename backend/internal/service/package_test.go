package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestPackageSnapshotAuthoritySchema(t *testing.T) {
 p := testutil.Postgres(t)
 for _, name := range []string{"package_catalog", "package_versions", "package_snapshots", "subscriptions", "traffic_sources", "billing_snapshots"} {
  var found bool
  if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&found); e != nil { t.Fatal(e) }
  if !found { t.Errorf("immutable package authority table %s missing", name) }
 }
}
