package db

import (
	"context"
	"database/sql"
	"embed"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate is an explicit administration command, never called by server startup.
func Migrate(ctx context.Context, url string) error {
	return migrate(ctx, url, 0)
}

// MigrateTo supports upgrade verification from an earlier committed schema.
func MigrateTo(ctx context.Context, url string, version int64) error {
	return migrate(ctx, url, version)
}

func migrate(ctx context.Context, url string, version int64) error {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer conn.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if version > 0 {
		return goose.UpToContext(ctx, conn, "migrations", version)
	}
	return goose.UpContext(ctx, conn, "migrations")
}
