package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

// RecoveryCreditFence is a direct authenticated backend decision, not a Hub
// claim of R. Every installed original-source child must occur in Credits.
// Installing the fence grants no bytes and never releases existing liability.
type RecoveryCreditFence struct {
	Version        int                   `json:"version"`
	ID             string                `json:"fence_id"`
	RecoveryID     string                `json:"recovery_id"`
	BackendID      string                `json:"backend_id"`
	HubID          string                `json:"hub_id"`
	NodeID         string                `json:"node_id"`
	BootID         string                `json:"boot_id"`
	ParentID       string                `json:"parent_authorization_id"`
	SnapshotID     string                `json:"billing_snapshot_id"`
	ManifestSHA256 string                `json:"manifest_sha256"`
	SealedSequence int64                 `json:"sealed_sequence"`
	IssuedAt       time.Time             `json:"issued_at"`
	ExpiresAt      time.Time             `json:"expires_at"`
	FromEpoch      uint64                `json:"from_epoch"`
	ToEpoch        uint64                `json:"to_epoch"`
	OriginalRaw    uint64                `json:"original_raw"`
	Issued         uint64                `json:"issued"`
	Remaining      uint64                `json:"remaining"`
	Root           ShareReference        `json:"original_credit_root"`
	Credits        []RecoveryCreditEntry `json:"credits"`
}
type RecoveryCreditEntry struct {
	GrantID string `json:"grant_id"`
	Amount  uint64 `json:"amount"`
}
type RecoveryCreditFenceReceipt struct {
	Version    int                   `json:"version"`
	ID         string                `json:"fence_id"`
	RecoveryID string                `json:"recovery_id"`
	BackendID  string                `json:"backend_id"`
	NodeID     string                `json:"node_id"`
	BootID     string                `json:"boot_id"`
	Digest     string                `json:"payload_sha256"`
	Installed  []RecoveryCreditEntry `json:"installed_credits"`
}

func DecodeRecoveryCreditFence(raw []byte) (RecoveryCreditFence, error) {
	var p RecoveryCreditFence
	if len(raw) == 0 || len(raw) > 1<<20 {
		return p, ErrSourceProvision
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, ErrSourceProvision
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(raw, canonical) {
		return RecoveryCreditFence{}, ErrSourceProvision
	}
	for _, id := range []string{p.ID, p.RecoveryID, p.BackendID, p.HubID, p.NodeID, p.BootID, p.ParentID, p.SnapshotID} {
		if !validIssuerUUID(id) {
			return RecoveryCreditFence{}, ErrSourceProvision
		}
	}
	hash, e := hex.DecodeString(p.ManifestSHA256)
	if p.Version != 1 || p.SealedSequence < 0 || p.SealedSequence == math.MaxInt64 || e != nil || len(hash) != 32 || hex.EncodeToString(hash) != p.ManifestSHA256 || p.FromEpoch < 1 || p.FromEpoch >= math.MaxInt64 || p.ToEpoch != p.FromEpoch+1 || p.OriginalRaw < 1 || p.OriginalRaw > math.MaxInt64 || p.Issued > p.OriginalRaw || p.Remaining != p.OriginalRaw-p.Issued || p.IssuedAt.IsZero() || p.IssuedAt.Location() != time.UTC || p.ExpiresAt.Location() != time.UTC || !p.ExpiresAt.After(p.IssuedAt) || p.ExpiresAt.Sub(p.IssuedAt) > 5*time.Minute || p.Credits == nil || len(p.Credits) > 4096 {
		return RecoveryCreditFence{}, ErrSourceProvision
	}
	r := p.Root
	if ValidateShareReference(r) != nil || r.ParentID != "" || r.ParentRevision != 0 || r.NodeID != p.NodeID || r.IssuerID != p.HubID || r.SignerEpoch != p.FromEpoch || !p.IssuedAt.Before(r.ExpiresAt) {
		return RecoveryCreditFence{}, ErrSourceProvision
	}
	total := uint64(0)
	previous := ""
	for _, c := range p.Credits {
		if !validIssuerUUID(c.GrantID) || c.GrantID <= previous || c.Amount == 0 || c.Amount > p.OriginalRaw-total {
			return RecoveryCreditFence{}, ErrSourceProvision
		}
		total += c.Amount
		previous = c.GrantID
	}
	if total != p.Issued {
		return RecoveryCreditFence{}, ErrSourceProvision
	}
	return p, nil
}

// ApplyRecoveryCreditFence requires the actual current Node boot and backend
// identity from a pinned direct channel. The receipt is emitted only after FULL.
// A fresh/cold Node cannot prove an old incarnation by presenting this payload.
func (s *Store) ApplyRecoveryCreditFence(ctx context.Context, node, boot, backend string, raw []byte, now IssuerTime) (RecoveryCreditFenceReceipt, error) {
	var out RecoveryCreditFenceReceipt
	p, e := DecodeRecoveryCreditFence(raw)
	if e != nil {
		return out, e
	}
	if s.owner != "agent" || p.NodeID != node || p.BootID != boot || p.BackendID != backend {
		return out, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var previous, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT payload,receipt FROM recovery_credit_fences WHERE node_id=? AND fence_id=?`, node, p.ID).Scan(&previous, &receipt)
	if e == nil {
		if !bytes.Equal(previous, raw) || json.Unmarshal(receipt, &out) != nil {
			return RecoveryCreditFenceReceipt{}, ErrAuthorizationUpdateConflict
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e = checkIssuerTime(now); e != nil {
		return out, e
	}
	if now.Latest.Sub(now.Earliest) > 10*time.Second || now.Earliest.Before(p.IssuedAt) || !now.Latest.Before(p.ExpiresAt) || !now.Latest.Before(p.Root.ExpiresAt) || !now.Latest.Before(p.Root.HardDeadline) {
		return out, ErrAuthorityTimeUncertain
	}
	var anchor, anchorDigest []byte
	var owner string
	if e = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,backend_id FROM share_source_anchors WHERE node_id=? AND source_id=? AND revision=?`, node, p.Root.SourceID, strconv.FormatUint(p.Root.Revision, 10)).Scan(&anchor, &anchorDigest, &owner); e != nil {
		return out, ErrAuthorizationDenied
	}
	canonical, _ := json.Marshal(p.Root)
	anchorHash := sha256.Sum256(anchor)
	if !bytes.Equal(anchor, canonical) || !bytes.Equal(anchorHash[:], anchorDigest) || owner != backend {
		return out, ErrAuthorizationDenied
	}
	initialEpoch := p.Root.SignerEpoch
	var ownerID string
	e = tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_source_owners WHERE node_id=? AND source_id=?`, node, p.Root.SourceID).Scan(&ownerID)
	if e == nil {
		original, _, e := readAuthorizationTx(ctx, tx, node, ownerID)
		if e != nil {
			return out, e
		}
		initialEpoch = original.SignerEpoch
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	current, e := sourceEpochTx(ctx, tx, node, p.Root.SourceID, initialEpoch)
	if e != nil || current != p.FromEpoch {
		return out, ErrAuthorityHandoffRequired
	}
	// Earlier fences form a unique adjacent chain. Their forensic replay never
	// lowers the latest floor or changes previously granted credit.
	var latest string
	e = tx.QueryRowContext(ctx, `SELECT to_epoch FROM recovery_credit_fences WHERE node_id=? AND source_id=? ORDER BY length(to_epoch) DESC,to_epoch DESC LIMIT 1`, node, p.Root.SourceID).Scan(&latest)
	if e == nil {
		v, e := strconv.ParseUint(latest, 10, 64)
		if e != nil || v != p.FromEpoch {
			return out, ErrAuthorityHandoffRequired
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	expected := map[string]uint64{}
	for _, c := range p.Credits {
		expected[c.GrantID] = c.Amount
	}
	installed := []RecoveryCreditEntry{}
	rows, e := tx.QueryContext(ctx, `SELECT authorization_id,parent_authorization_id,issuer_id,boot_id,billing_snapshot_id,amount_bytes,signer_epoch,rule_id,rule_revision,path_generation FROM authorization_records WHERE node_id=? AND source_id=? AND kind='credit_grant' ORDER BY authorization_id`, node, p.Root.SourceID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var id, parent, issuer, oldBoot, snapshot, amount, epoch, rule, revision, path string
		if e = rows.Scan(&id, &parent, &issuer, &oldBoot, &snapshot, &amount, &epoch, &rule, &revision, &path); e != nil {
			break
		}
		n, pe := strconv.ParseUint(amount, 10, 64)
		v, ee := strconv.ParseUint(epoch, 10, 64)
		if pe != nil || ee != nil || n == 0 || expected[id] != n || parent != p.ParentID || issuer != p.HubID || oldBoot != boot || snapshot != p.SnapshotID || v == 0 || v > p.FromEpoch || rule != p.Root.RuleID || revision != strconv.FormatUint(p.Root.RuleRevision, 10) || path != strconv.FormatUint(p.Root.PathGeneration, 10) {
			e = ErrAuthorizationDenied
			break
		}
		installed = append(installed, RecoveryCreditEntry{GrantID: id, Amount: n})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	sum := sha256.Sum256(raw)
	out = RecoveryCreditFenceReceipt{Version: 1, ID: p.ID, RecoveryID: p.RecoveryID, BackendID: backend, NodeID: node, BootID: boot, Digest: hex.EncodeToString(sum[:]), Installed: installed}
	receipt, e = json.Marshal(out)
	if e != nil {
		return RecoveryCreditFenceReceipt{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO recovery_credit_fences(node_id,fence_id,source_id,from_epoch,to_epoch,payload,receipt) VALUES(?,?,?,?,?,?,?)`, node, p.ID, p.Root.SourceID, strconv.FormatUint(p.FromEpoch, 10), strconv.FormatUint(p.ToEpoch, 10), raw, receipt); e != nil {
		return RecoveryCreditFenceReceipt{}, e
	}
	if e = tx.Commit(); e != nil {
		return RecoveryCreditFenceReceipt{}, e
	}
	return out, nil
}

// A fence blocks delayed old credit and caps all historical plus new children
// by the one original backend raw budget. Existing installed grants may drain.
func checkRecoveryCreditFenceTx(ctx context.Context, tx *sql.Tx, node string, r AuthorizationRecord) error {
	if r.Kind != "credit_grant" {
		return nil
	}
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT payload FROM recovery_credit_fences WHERE node_id=? AND source_id=? ORDER BY length(to_epoch) DESC,to_epoch DESC LIMIT 1`, node, r.SourceID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	p, e := DecodeRecoveryCreditFence(raw)
	if e != nil {
		return e
	}
	if r.SignerEpoch != p.ToEpoch || r.IssuerID != p.HubID || r.BootID != p.BootID || r.ParentAuthorizationID != p.ParentID || r.BillingSnapshotID != p.SnapshotID || r.RuleID != p.Root.RuleID || r.RuleRevision != p.Root.RuleRevision || r.PathGeneration != p.Root.PathGeneration {
		return ErrAuthorityHandoffRequired
	}
	rows, e := tx.QueryContext(ctx, `SELECT amount_bytes,signer_epoch FROM authorization_records WHERE node_id=? AND source_id=? AND kind='credit_grant'`, node, r.SourceID)
	if e != nil {
		return e
	}
	total, newTotal := uint64(0), uint64(0)
	for rows.Next() {
		var text, epoch string
		if e = rows.Scan(&text, &epoch); e != nil {
			break
		}
		n, pe := strconv.ParseUint(text, 10, 64)
		v, ee := strconv.ParseUint(epoch, 10, 64)
		if pe != nil || ee != nil || n > p.OriginalRaw-total {
			e = ErrAuthorizationDenied
			break
		}
		total += n
		if v == p.ToEpoch {
			if n > p.Remaining-newTotal {
				e = ErrAuthorizationDenied
				break
			}
			newTotal += n
		} else if v > p.FromEpoch {
			e = ErrAuthorizationDenied
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	if r.AmountBytes > p.OriginalRaw-total || r.AmountBytes > p.Remaining-newTotal {
		return ErrAuthorizationDenied
	}
	return nil
}

func checkRecoveryHandoffFenceTx(ctx context.Context, tx *sql.Tx, u AuthorizationUpdate, h EpochHandoff) error {
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT payload FROM recovery_credit_fences WHERE node_id=? AND source_id=? ORDER BY length(to_epoch) DESC,to_epoch DESC LIMIT 1`, u.NodeID, h.SourceID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	p, e := DecodeRecoveryCreditFence(raw)
	if e != nil {
		return e
	}
	if u.BootID != p.BootID || u.IssuerID != p.HubID || h.TransferID != p.RecoveryID || h.ParentID != p.ParentID || h.From != p.FromEpoch || h.To != p.ToEpoch || h.Remaining != p.Remaining || h.SealedSequence != p.SealedSequence || h.RecordSHA256 != p.ManifestSHA256 {
		return ErrAuthorityHandoffRequired
	}
	return nil
}

func (s *Store) migrateRecoveryCreditFences() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`CREATE TABLE IF NOT EXISTS recovery_credit_fences (
 node_id TEXT NOT NULL,fence_id TEXT NOT NULL,source_id TEXT NOT NULL,
 from_epoch TEXT NOT NULL,to_epoch TEXT NOT NULL,payload BLOB NOT NULL,receipt BLOB NOT NULL,
 PRIMARY KEY(node_id,fence_id),UNIQUE(node_id,source_id,to_epoch));
 CREATE TRIGGER IF NOT EXISTS recovery_credit_fences_no_update BEFORE UPDATE ON recovery_credit_fences BEGIN SELECT RAISE(ABORT,'immutable recovery credit fence'); END;
 CREATE TRIGGER IF NOT EXISTS recovery_credit_fences_no_delete BEFORE DELETE ON recovery_credit_fences BEGIN SELECT RAISE(ABORT,'immutable recovery credit fence'); END;
 INSERT INTO schema_meta(key,value) VALUES('recovery_credit_fences_version','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value;`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
