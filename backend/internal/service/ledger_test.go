package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestLedgerSourceAuthoritySchema(t *testing.T) {
 p := testutil.Postgres(t)
 for _, table := range []string{"source_ledgers", "ledger_events", "ledger_projections", "reservations", "settlements", "disputes", "unknown_liabilities"} {
  var found bool
  if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&found); e != nil { t.Fatal(e) }
  if !found { t.Errorf("source-local immutable ledger table %s missing", table) }
 }
}
