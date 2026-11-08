package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"vortex/contracts"
)

// StopUninstalledCreditGrant is a local primitive for a verified original Hub
// issue targeting the current Node boot. The caller must authenticate that
// command and check the current boot; an arbitrary/new boot cannot attest that
// another incarnation never ran. No time or missing transport ACK proves this.
// The original grant identity, zero-use witness and permanent late-install
// revocation commit together. It cannot reinterpret an installed grant as absent.
func (s *Store) StopUninstalledCreditGrant(ctx context.Context, node, boot string, expected AuthorizationRecord) (CreditGrantFinal, error) {
	var zero CreditGrantFinal
	if s.owner != "agent" || node == "" || boot == "" || expected.BootID != boot || expected.IssuerID == "" || expected.AuthorizationID == "" || expected.Kind != "credit_grant" || expected.SourceID == "" || expected.ParentAuthorizationID == "" || expected.RuleID == "" || expected.RuleRevision == 0 || expected.PathGeneration == 0 || expected.SignerEpoch == 0 || expected.BillingSnapshotID == "" || expected.AmountBytes == 0 || expected.RateBPS != 0 || expected.Connections != 0 {
		return zero, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if final, found, err := uninstalledCreditFinalTx(ctx, tx, node, expected.AuthorizationID); err != nil {
		return zero, err
	} else if found {
		if !sameIssuedCreditGrant(final.Grant, expected) {
			return zero, ErrAuthorizationUpdateConflict
		}
		return final, tx.Commit()
	}
	_, _, err = readAuthorizationTx(ctx, tx, node, expected.AuthorizationID)
	if !errors.Is(err, sql.ErrNoRows) {
		if err == nil {
			err = ErrAuthorizationUpdateConflict
		}
		return zero, err
	}
	var unknown int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM unknown_usage_batches WHERE node_id=? AND grant_id=?`, node, expected.AuthorizationID).Scan(&unknown); err != nil {
		return zero, err
	}
	if unknown != 0 {
		return zero, ErrCreditGrantUnsettled
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grant_stops(node_id,authorization_id,boot_id,issuer_id) VALUES(?,?,?,?)`, node, expected.AuthorizationID, boot, expected.IssuerID); err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO authorization_revocations(node_id,authorization_id,signer_epoch,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, node, expected.AuthorizationID, strconv.FormatUint(expected.SignerEpoch, 10), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return zero, err
	}
	digest := sha256.Sum256([]byte("vortex/stopped-credit-usage/v1\n"))
	final := CreditGrantFinal{NodeID: node, Grant: expected, NeverInstalled: true, Unused: expected.AmountBytes, UsageSHA256: hex.EncodeToString(digest[:])}
	final.KnownUsage, err = contracts.BuildKnownUsageInventory(nil)
	if err != nil {
		return zero, err
	}
	raw, err := json.Marshal(final)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO uninstalled_credit_grant_finals(node_id,authorization_id,payload) VALUES(?,?,?)`, node, expected.AuthorizationID, raw); err != nil {
		return zero, err
	}
	return final, tx.Commit()
}

// ShareRevision is a local reference-head observation assigned at installation,
// not part of the original immutable issuer grant. All issued fields must match.
func sameIssuedCreditGrant(a, b AuthorizationRecord) bool {
	a.ShareRevision, b.ShareRevision = 0, 0
	return a == b
}

func uninstalledCreditFinalTx(ctx context.Context, tx *sql.Tx, node, grant string) (CreditGrantFinal, bool, error) {
	var final CreditGrantFinal
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload FROM uninstalled_credit_grant_finals WHERE node_id=? AND authorization_id=?`, node, grant).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return final, false, nil
	}
	if err != nil {
		return final, false, err
	}
	if json.Unmarshal(raw, &final) != nil || final.NodeID != node || final.Grant.AuthorizationID != grant || !final.NeverInstalled || final.Used != 0 || final.Unused != final.Grant.AmountBytes || final.BatchCount != 0 {
		return CreditGrantFinal{}, false, ErrUsageBatchConflict
	}
	return final, true, nil
}

// A delayed issue can be retained as revoked history but can never change the
// scope or amount covered by an earlier never-installed witness.
func checkUninstalledCreditFinalTx(ctx context.Context, tx *sql.Tx, node string, record AuthorizationRecord) error {
	final, found, err := uninstalledCreditFinalTx(ctx, tx, node, record.AuthorizationID)
	if err != nil {
		return err
	}
	if found && !sameIssuedCreditGrant(record, final.Grant) {
		return ErrAuthorizationUpdateConflict
	}
	return nil
}
