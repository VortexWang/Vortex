package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
)

// OpenReader observes an existing WAL without configuring or migrating it.
// It cannot create a file or mutate authority. The owning Agent/Hub remains
// the only writer; diagnostic readers must not run startup DDL against it.
func OpenReader(path, owner string) (*Store, error) {
	if path == "" || (owner != "agent" && owner != "hub") {
		return nil, fmt.Errorf("store: existing ledger path and role required")
	}
	dsn, err := ledgerDSN(path, true)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, owner: owner}
	var schema string
	if err := db.QueryRowContext(context.Background(), `SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&schema); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func ledgerDSN(path string, reader bool) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{"_pragma": {"busy_timeout(5000)"}}
	if reader {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	} else {
		// Acquire the write reservation before reading mutable authority.
		// busy_timeout cannot repair a stale deferred WAL snapshot promoted
		// after another connection commits. Read-only transactions stay deferred.
		q.Set("_txlock", "immediate")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
