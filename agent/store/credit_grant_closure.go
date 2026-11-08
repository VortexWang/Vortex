package store

import (
	"bytes"
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

var ErrCreditGrantUnsettled = errors.New("store: stopped credit grant has unresolved writes")

// CreditGrantFinal is local durable evidence, not a transport acknowledgement or
// permission to release a backend hold. The receiver must authenticate the Node
// and independently match the complete issuer and usage inventories.
type CreditGrantFinal struct {
	NodeID         string
	Grant          AuthorizationRecord
	NeverInstalled bool `json:",omitempty"`
	Used           uint64
	Unused         uint64
	BatchCount     uint64
	UsageSHA256    string
	KnownUsage     contracts.KnownUsageInventory
}

// StopCreditGrant permanently fences further pre-write reservations. Existing
// tickets may still write and settle; stopping never resolves them, closes their
// sessions, returns quota or claims the Engine has stopped. Final evidence is a
// separate transaction after every ticket has a known terminal outcome.
func (s *Store) StopCreditGrant(ctx context.Context, node, boot, issuer, grant string) error {
	return s.stopCreditGrant(ctx, node, boot, issuer, grant, nil)
}

// StopMatchingCreditGrant also pins every original issuer field. ShareRevision
// alone is a local reference observation and is not part of the issued grant.
func (s *Store) StopMatchingCreditGrant(ctx context.Context, node, boot string, expected AuthorizationRecord) error {
	return s.stopCreditGrant(ctx, node, boot, expected.IssuerID, expected.AuthorizationID, &expected)
}

func (s *Store) stopCreditGrant(ctx context.Context, node, boot, issuer, grant string, expected *AuthorizationRecord) error {
	if s.owner != "agent" || node == "" || boot == "" || issuer == "" || grant == "" {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, _, err := readAuthorizationTx(ctx, tx, node, grant)
	if err != nil {
		return err
	}
	if r.Kind != "credit_grant" || r.BootID != boot || r.IssuerID != issuer {
		return ErrAuthorizationDenied
	}
	if expected != nil && !sameIssuedCreditGrant(r, *expected) {
		return ErrAuthorizationUpdateConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grant_stops(node_id,authorization_id,boot_id,issuer_id) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, node, grant, boot, issuer); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO authorization_revocations(node_id,authorization_id,signer_epoch,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, node, grant, strconv.FormatUint(r.SignerEpoch, 10), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE authorization_records SET state='revoked' WHERE node_id=? AND authorization_id=?`, node, grant); err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizeStoppedCreditGrant observes the fence, all original tickets and their
// exact usage in one SQLite transaction. A reserved ticket can still write, and
// an unknown ticket can hide consumption, so neither permits final evidence.
// Expiry, session close, ACK and wall time are deliberately irrelevant here.
func (s *Store) FinalizeStoppedCreditGrant(ctx context.Context, node, boot, issuer, grant string) (CreditGrantFinal, error) {
	var zero CreditGrantFinal
	if s.owner != "agent" || node == "" || boot == "" || issuer == "" || grant == "" {
		return zero, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if final, found, err := uninstalledCreditFinalTx(ctx, tx, node, grant); err != nil {
		return zero, err
	} else if found {
		if final.Grant.BootID != boot || final.Grant.IssuerID != issuer {
			return zero, ErrAuthorizationDenied
		}
		return final, tx.Commit()
	}
	r, state, err := readAuthorizationTx(ctx, tx, node, grant)
	if err != nil {
		return zero, err
	}
	if r.Kind != "credit_grant" || r.BootID != boot || r.IssuerID != issuer || state != "revoked" {
		return zero, ErrAuthorizationDenied
	}
	var stopped int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grant_stops WHERE node_id=? AND authorization_id=? AND boot_id=? AND issuer_id=?`, node, grant, boot, issuer).Scan(&stopped); err != nil {
		return zero, err
	}
	if stopped != 1 {
		return zero, ErrAuthorizationDenied
	}
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM credit_grant_finals WHERE node_id=? AND authorization_id=?`, node, grant).Scan(&previous)
	if err == nil {
		var final CreditGrantFinal
		if json.Unmarshal(previous, &final) != nil || final.NodeID != node || final.Grant != r {
			return zero, ErrUsageBatchConflict
		}
		return final, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	var unknown int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM unknown_usage_batches WHERE node_id=? AND grant_id=?`, node, grant).Scan(&unknown); err != nil {
		return zero, err
	}
	if unknown != 0 {
		return zero, ErrCreditGrantUnsettled
	}
	var source, initialText, remainingText string
	if err = tx.QueryRowContext(ctx, `SELECT source_id,initial_bytes,remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, node, grant).Scan(&source, &initialText, &remainingText); err != nil {
		return zero, err
	}
	initial, e1 := strconv.ParseUint(initialText, 10, 64)
	remaining, e2 := strconv.ParseUint(remainingText, 10, 64)
	if e1 != nil || e2 != nil || initial != r.AmountBytes || remaining > initial || source != r.SourceID {
		return zero, ErrUsageBatchConflict
	}
	final := CreditGrantFinal{NodeID: node, Grant: r, Unused: remaining}
	var known []contracts.KnownUsageBatch
	digest := sha256.New()
	_, _ = digest.Write([]byte("vortex/stopped-credit-usage/v1\n"))
	rows, err := tx.QueryContext(ctx, `SELECT h.batch_id,h.source_id,h.reserved_bytes,h.state,u.boot_id,u.snapshot_id,u.rule_id,u.flow_key,u.direction,u.reserved_bytes,u.state,u.used_bytes,b.payload,b.payload_sha256,b.unknown_upper_bound,
 EXISTS(SELECT 1 FROM unknown_usage_conflicts c WHERE c.node_id=h.node_id AND c.batch_id=h.batch_id)
 FROM authorization_usage_holds h LEFT JOIN usage_reservations u ON u.node_id=h.node_id AND u.batch_id=h.batch_id
 LEFT JOIN usage_batches b ON b.node_id=h.node_id AND b.batch_id=h.batch_id
 WHERE h.node_id=? AND h.authorization_id=? ORDER BY h.batch_id`, node, grant)
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	for rows.Next() {
		var batch, heldSource, heldAmount, heldState, origin, snapshot, rule, flow, direction, amount, outcome, usedText string
		var payload, payloadDigest []byte
		var upper sql.NullString
		var conflict bool
		if err = rows.Scan(&batch, &heldSource, &heldAmount, &heldState, &origin, &snapshot, &rule, &flow, &direction, &amount, &outcome, &usedText, &payload, &payloadDigest, &upper, &conflict); err != nil {
			return zero, err
		}
		if heldState == "reserved" || heldState == "unknown" || outcome == "reserved" || outcome == "unknown" || conflict {
			return zero, ErrCreditGrantUnsettled
		}
		reserved, e1 := strconv.ParseUint(amount, 10, 64)
		used, e2 := strconv.ParseUint(usedText, 10, 64)
		if e1 != nil || e2 != nil || reserved == 0 || used > reserved || used > initial-final.Used || heldAmount != amount || heldState != outcome || heldSource != source || origin != boot || snapshot != r.BillingSnapshotID || rule != r.RuleID || flow == "" || (direction != "egress" && direction != "ingress") {
			return zero, ErrUsageBatchConflict
		}
		payloadHash := ""
		switch outcome {
		case "committed":
			sum := sha256.Sum256(payload)
			if len(payload) == 0 || !upper.Valid || upper.String != "0" || !bytes.Equal(sum[:], payloadDigest) {
				return zero, ErrUsageBatchConflict
			}
			payloadHash = hex.EncodeToString(sum[:])
			known = append(known, contracts.KnownUsageBatch{BatchID: batch, SHA256: payloadHash})
		case "returned":
			if used != 0 || upper.Valid || len(payload) != 0 || len(payloadDigest) != 0 {
				return zero, ErrUsageBatchConflict
			}
		default:
			return zero, ErrUsageBatchConflict
		}
		// Fixed-position canonical JSON plus newline is unambiguous even when
		// identifiers contain delimiters. The payload hash binds original bytes.
		entry, err := json.Marshal([]string{batch, flow, direction, amount, outcome, usedText, payloadHash})
		if err != nil {
			return zero, err
		}
		_, _ = digest.Write(entry)
		_, _ = digest.Write([]byte{'\n'})
		final.Used += used
		final.BatchCount++
	}
	if err = rows.Err(); err != nil {
		return zero, err
	}
	if err = rows.Close(); err != nil {
		return zero, err
	}
	if final.Used != initial-remaining {
		return zero, ErrUsageBatchConflict
	}
	final.UsageSHA256 = hex.EncodeToString(digest.Sum(nil))
	final.KnownUsage, err = contracts.BuildKnownUsageInventory(known)
	if err != nil {
		return zero, err
	}
	payload, err := json.Marshal(final)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grant_finals(node_id,authorization_id,payload) VALUES(?,?,?)`, node, grant, payload); err != nil {
		return zero, err
	}
	return final, tx.Commit()
}

func (s *Store) migrateCreditGrantClosures() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS credit_grant_stops(node_id TEXT NOT NULL,authorization_id TEXT NOT NULL,boot_id TEXT NOT NULL,issuer_id TEXT NOT NULL,PRIMARY KEY(node_id,authorization_id));
CREATE TABLE IF NOT EXISTS credit_grant_finals(node_id TEXT NOT NULL,authorization_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(node_id,authorization_id));
CREATE TABLE IF NOT EXISTS uninstalled_credit_grant_finals(node_id TEXT NOT NULL,authorization_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(node_id,authorization_id));
CREATE TRIGGER IF NOT EXISTS credit_grant_stop_no_update BEFORE UPDATE ON credit_grant_stops BEGIN SELECT RAISE(ABORT,'immutable credit stop'); END;
CREATE TRIGGER IF NOT EXISTS credit_grant_stop_no_delete BEFORE DELETE ON credit_grant_stops BEGIN SELECT RAISE(ABORT,'immutable credit stop'); END;
CREATE TRIGGER IF NOT EXISTS credit_grant_final_no_update BEFORE UPDATE ON credit_grant_finals BEGIN SELECT RAISE(ABORT,'immutable credit final'); END;
CREATE TRIGGER IF NOT EXISTS credit_grant_final_no_delete BEFORE DELETE ON credit_grant_finals BEGIN SELECT RAISE(ABORT,'immutable credit final'); END;
CREATE TRIGGER IF NOT EXISTS uninstalled_credit_final_no_update BEFORE UPDATE ON uninstalled_credit_grant_finals BEGIN SELECT RAISE(ABORT,'immutable uninstalled credit final'); END;
CREATE TRIGGER IF NOT EXISTS uninstalled_credit_final_no_delete BEFORE DELETE ON uninstalled_credit_grant_finals BEGIN SELECT RAISE(ABORT,'immutable uninstalled credit final'); END;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
