package service_test

import (
	"context"
	"github.com/google/uuid"
	"net/url"
	"os"
	"strings"
	"testing"
	"vortex/backend/internal/testutil"
)

// The production due worker handles one globally ordered task per call. Tests
// asserting a particular task need their own real schema, not a shared backlog.
func isolatePackageAuthority(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	admin := testutil.Postgres(t)
	schema := "package_authority_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	query := u.Query()
	query.Set("search_path", schema)
	query.Set("application_name", schema)
	u.RawQuery = query.Encode()
	t.Setenv("PHASE6_DATABASE_URL", u.String())
}
