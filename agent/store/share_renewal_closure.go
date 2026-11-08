package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"vortex/contracts"
)

// ApplyShareRenewalClosure commits denial tombstones even for allocations not
// yet installed. A late allocation cannot resurrect issuance after this FULL
// receipt. Closing needs no trusted clock and releases no funds or live leases.
func (s *Store) ApplyShareRenewalClosure(ctx context.Context, hub, boot, backend string, raw []byte) (contracts.ShareRenewalClosureReceipt, error) {
	var zero contracts.ShareRenewalClosureReceipt
	c, e := contracts.DecodeShareRenewalClosure(raw)
	if e != nil {
		return zero, e
	}
	if s.owner != "hub" || c.HubID != hub || c.HubBootID != boot || c.BackendID != backend {
		return zero, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	var prior, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT payload,receipt FROM share_renewal_closures WHERE command_id=?`, c.ID).Scan(&prior, &receipt)
	if e == nil {
		if !bytes.Equal(prior, raw) {
			return zero, ErrAuthorizationUpdateConflict
		}
		r, e := contracts.DecodeShareRenewalClosureReceipt(receipt)
		if e != nil {
			return zero, e
		}
		return r, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	for _, id := range c.DelegationIDs {
		var installed []byte
		var epoch string
		e = tx.QueryRowContext(ctx, `SELECT payload,signer_epoch FROM issuer_delegations WHERE delegation_id=?`, id).Scan(&installed, &epoch)
		if e == nil {
			var d HubDelegation
			if json.Unmarshal(installed, &d) != nil || d.ID != id || d.HubID != c.HubID || d.NodeID != c.NodeID || d.BootID != c.BootID || d.SignerEpoch != uint64(c.SignerEpoch) || epoch != strconv.FormatInt(c.SignerEpoch, 10) {
				return zero, ErrAuthorizationDenied
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO closed_share_delegations(delegation_id,command_id) VALUES(?,?) ON CONFLICT(delegation_id) DO NOTHING`, id, c.ID); e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE issuer_delegations SET state='closed' WHERE delegation_id=?`, id); e != nil {
			return zero, e
		}
	}
	r := contracts.ShareRenewalClosureReceipt{Version: 1, ID: c.ID, CommandDigest: contracts.SharePayloadDigest(raw)}
	encoded, e := json.Marshal(r)
	if e != nil {
		return zero, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO share_renewal_closures(command_id,payload,receipt) VALUES(?,?,?)`, c.ID, raw, encoded); e != nil {
		return zero, e
	}
	if e = tx.Commit(); e != nil {
		return zero, e
	}
	return r, nil
}

func (s *Store) migrateShareRenewalClosures() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS share_renewal_closures(command_id TEXT PRIMARY KEY,payload BLOB NOT NULL,receipt BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS closed_share_delegations(delegation_id TEXT PRIMARY KEY,command_id TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS share_closure_no_update BEFORE UPDATE ON share_renewal_closures BEGIN SELECT RAISE(ABORT,'immutable closure'); END;
CREATE TRIGGER IF NOT EXISTS share_closure_no_delete BEFORE DELETE ON share_renewal_closures BEGIN SELECT RAISE(ABORT,'immutable closure'); END;
CREATE TRIGGER IF NOT EXISTS closed_delegation_no_update BEFORE UPDATE ON closed_share_delegations BEGIN SELECT RAISE(ABORT,'immutable closure'); END;
CREATE TRIGGER IF NOT EXISTS closed_delegation_no_delete BEFORE DELETE ON closed_share_delegations BEGIN SELECT RAISE(ABORT,'immutable closure'); END;`); e != nil {
		return e
	}
	return tx.Commit()
}
