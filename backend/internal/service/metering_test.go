package service_test

import (
 "context"
 "testing"
 "vortex/backend/internal/testutil"
)

func TestUsageBatchIdempotencyAuthoritySchema(t *testing.T) {
 p:=testutil.Postgres(t)
 var found bool
 if e:=p.QueryRow(context.Background(),"SELECT to_regclass('usage_batches') IS NOT NULL").Scan(&found);e!=nil{t.Fatal(e)}
 if !found{t.Fatal("immutable usage tuple/digest authority missing")}
}
