package service_test

import (
	"context"
	"testing"
	"vortex/backend/internal/testutil"
)

func TestShareLeaseAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	for _, table := range []string{"share_chains", "share_generations", "lease_expectations", "node_fencing", "share_renewal_authorities"} {
		var present bool
		if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); e != nil {
			t.Fatal(e)
		}
		if !present {
			t.Errorf("durable share/lease authority table %s missing", table)
		}
	}
}
