package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestPortReservationProofSchema(t *testing.T) {
 pool:=testutil.Postgres(t)
 for _,table:=range []string{"port_allocations","port_reservations","release_proofs"} {
  var exists bool
  if e:=pool.QueryRow(context.Background(),"SELECT to_regclass($1) IS NOT NULL",table).Scan(&exists);e!=nil{t.Fatal(e)}
  if !exists{t.Errorf("port allocation authority table %s missing",table)}
 }
}
