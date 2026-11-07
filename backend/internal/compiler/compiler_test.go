package compiler_test

import (
	"context"
	"testing"
	"vortex/backend/internal/testutil"
)

func TestCompileArtifactImmutableSchema(t *testing.T){
	pool:=testutil.Postgres(t)
	var exists bool
	if e:=pool.QueryRow(context.Background(),"SELECT to_regclass('rule_artifacts') IS NOT NULL").Scan(&exists);e!=nil{t.Fatal(e)}
	if !exists{t.Fatal("immutable per-node compiled artifact storage missing")}
}
