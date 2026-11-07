package service_test

import (
	"context"
	"testing"
	"vortex/backend/internal/testutil"
)

func TestRuleRevisionSchema(t *testing.T) {
	pool := testutil.Postgres(t)
	for _, table := range []string{"rule_heads", "rule_revisions", "rule_targets", "rule_segments", "rule_dependencies"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil { t.Fatal(err) }
		if !exists { t.Errorf("immutable rule authority table %s missing", table) }
	}
}
