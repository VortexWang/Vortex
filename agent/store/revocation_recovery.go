package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

func (s *Store) migrateScopedRevocations() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS authorization_scoped_revocations(
 node_id TEXT NOT NULL,issuer_id TEXT NOT NULL,boot_id TEXT NOT NULL,path_generation TEXT NOT NULL,
 authorization_id TEXT NOT NULL,signer_epoch TEXT NOT NULL,created_at TEXT NOT NULL,
 PRIMARY KEY(node_id,issuer_id,boot_id,path_generation,authorization_id));
 CREATE TRIGGER IF NOT EXISTS scoped_revocations_no_update BEFORE UPDATE ON authorization_scoped_revocations BEGIN SELECT RAISE(ABORT,'immutable scoped revocation'); END;
 CREATE TRIGGER IF NOT EXISTS scoped_revocations_no_delete BEFORE DELETE ON authorization_scoped_revocations BEGIN SELECT RAISE(ABORT,'immutable scoped revocation'); END;`)
	return err
}

// Older-path pure revocations can only narrow their authenticated issuer's
// original Node/boot/path. Unknown IDs receive scoped tombstones, never global
// ones. No floor, heartbeat, source handoff or fresh right is created here.
func olderRevocationOnlyTx(ctx context.Context, tx *sql.Tx, u AuthorizationUpdate, records []AuthorizationRecord, revoked []string) (bool, error) {
	if len(records) != 0 || len(u.Handoffs) != 0 || len(u.PolicyTransitions) != 0 || len(u.ShareReferences) != 0 || len(revoked) == 0 || u.IssuerID == "" {
		return false, nil
	}
	older := false
	for key, incoming := range map[string]uint64{"authorization_path_generation": u.PathGeneration, "authorization_fencing_floor": u.FencingGenerationFloor} {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key=?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		floor, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return false, ErrAuthorizationGenerationStale
		}
		older = older || incoming < floor
	}
	if !older {
		return false, nil
	}
	for _, id := range revoked {
		r, _, err := readAuthorizationTx(ctx, tx, u.NodeID, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if r.IssuerID != u.IssuerID || r.BootID != u.BootID || r.PathGeneration != u.PathGeneration || r.SignerEpoch > u.SignerEpoch {
			return false, ErrAuthorizationDenied
		}
	}
	return true, nil
}
