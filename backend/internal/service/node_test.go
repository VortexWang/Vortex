package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestNodeGroupAuthoritySchema(t *testing.T) {
 pool := testutil.Postgres(t)
 for _, table := range []string{"nodes", "node_groups", "node_group_members", "platform_grants", "grant_history", "segment_permits", "encrypted_credentials"} {
  var exists bool
  if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil { t.Fatal(err) }
  if !exists { t.Errorf("node authority table %s missing", table) }
 }
}
