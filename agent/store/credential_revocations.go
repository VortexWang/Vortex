package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (s *Store) migrateCredentialRevocations() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS credential_revocation_batches(issuer_id TEXT NOT NULL, revision INTEGER NOT NULL,payload BLOB NOT NULL, PRIMARY KEY(issuer_id,revision));
 INSERT INTO schema_meta(key,value) VALUES('credential_revocation_version','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// PersistCredentialRevocations stores additive batches before publication. An
// identical old revision is a no-op, never a rollback of the cumulative view.
func (s *Store) PersistCredentialRevocations(ctx context.Context, issuer string, r *controlv1.CredentialRevocations) error {
	if s.owner != "agent" || r == nil || issuer != r.IssuerId {
		return ErrAuthorizationDenied
	}
	payload, err := contracts.EncodeCredentialRevocations(r)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM credential_revocation_batches WHERE issuer_id=? AND revision=?`, issuer, r.Revision).Scan(&previous)
	if err == nil {
		if !bytes.Equal(payload, previous) {
			return ErrAuthorizationDenied
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var max uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM credential_revocation_batches WHERE issuer_id=?`, issuer).Scan(&max); err != nil {
		return err
	}
	if r.Revision <= max {
		return ErrAuthorizationDenied
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credential_revocation_batches(issuer_id,revision,payload) VALUES(?,?,?)`, issuer, r.Revision, payload); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) CredentialRevocations(ctx context.Context) ([]*controlv1.CredentialRevocations, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT issuer_id,revision,payload FROM credential_revocation_batches ORDER BY issuer_id,revision`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*controlv1.CredentialRevocations
	for rows.Next() {
		var issuer string
		var revision uint64
		var payload []byte
		if err = rows.Scan(&issuer, &revision, &payload); err != nil {
			return nil, err
		}
		r := new(controlv1.CredentialRevocations)
		if err = proto.Unmarshal(payload, r); err != nil {
			return nil, err
		}
		canonical, e := contracts.EncodeCredentialRevocations(r)
		if e != nil || !bytes.Equal(payload, canonical) || issuer != r.IssuerId || revision != r.Revision {
			return nil, fmt.Errorf("store: corrupt credential revocation batch")
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
