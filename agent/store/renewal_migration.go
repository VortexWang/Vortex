package store

import (
	"context"
	"database/sql"
)

// All additive share/renewal schema changes and their completion marker commit
// together under the already configured synchronous=FULL writer. Failure leaves
// old authority and columns intact; opening again can retry the entire step.
func (s *Store) migrateRenewalAuthority() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS issuer_renewals (
 request_id TEXT PRIMARY KEY NOT NULL,issuer_id TEXT NOT NULL,
 request BLOB NOT NULL,request_sha256 BLOB NOT NULL,
 result BLOB NOT NULL,result_sha256 BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS share_source_anchors (
 node_id TEXT NOT NULL,source_id TEXT NOT NULL,revision TEXT NOT NULL,
 backend_id TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(node_id,source_id,revision)
);
CREATE TABLE IF NOT EXISTS share_source_heads (
 node_id TEXT NOT NULL,source_id TEXT NOT NULL,revision TEXT NOT NULL,
 backend_id TEXT NOT NULL,PRIMARY KEY(node_id,source_id)
);
CREATE TABLE IF NOT EXISTS share_references (
 node_id TEXT NOT NULL,share_id TEXT NOT NULL,revision TEXT NOT NULL,
 parent_id TEXT NOT NULL,parent_revision TEXT NOT NULL,
 rate_bps TEXT NOT NULL,connections TEXT NOT NULL,
 payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(node_id,share_id,revision)
);
CREATE TABLE IF NOT EXISTS share_reference_heads (
 node_id TEXT NOT NULL,share_id TEXT NOT NULL,revision TEXT NOT NULL,
 PRIMARY KEY(node_id,share_id)
);
`); err != nil {
		return err
	}
	for _, col := range []struct{ table, name, decl string }{
		{"authorization_records", "delegation_id", "TEXT NOT NULL DEFAULT ''"},
		{"authorization_records", "share_reference_id", "TEXT NOT NULL DEFAULT ''"},
		{"authorization_records", "share_revision", "TEXT NOT NULL DEFAULT ''"},
		{"issuer_issues", "renewal_result", "BLOB NOT NULL DEFAULT X''"},
		{"renewal_requests", "trigger_authorization_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureRenewalColumnTx(tx, col.table, col.name, col.decl); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS renewal_trigger_once ON renewal_requests(node_id,trigger_authorization_id) WHERE trigger_authorization_id<>''`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_meta SET value='16' WHERE key='schema_version' AND CAST(value AS INTEGER)<16`); err != nil {
		return err
	}
	return tx.Commit()
}

// Identifiers and declarations are fixed migration literals, never wire input.
func ensureRenewalColumnTx(tx *sql.Tx, table, name, decl string) error {
	rows, err := tx.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var column, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if column == name {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + decl)
	return err
}
