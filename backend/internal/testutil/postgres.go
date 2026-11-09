package testutil

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
	"vortex/backend/db"
)

// Postgres always fails when the real database is unavailable. No skips or
// in-memory replacements may turn a missing authority service into a pass.
func Postgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("PHASE6_DATABASE_URL")
	if url == "" {
		t.Fatal("PHASE6_DATABASE_URL required; run tools/phase6/tracer_real.sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 8
	config.ConnConfig.ConnectTimeout = 3 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
