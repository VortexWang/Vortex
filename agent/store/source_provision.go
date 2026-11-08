package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// SourceProvision is direct backend provisioning, never Hub-issued runtime
// authority. These roots merely bound later authorizations; they grant no bytes.
type SourceProvision struct {
	Version   int              `json:"version"`
	ID        string           `json:"provision_id"`
	BackendID string           `json:"backend_id"`
	NodeID    string           `json:"node_id"`
	BootID    string           `json:"boot_id"`
	IssuedAt  time.Time        `json:"issued_at"`
	Roots     []ShareReference `json:"roots"`
}
type SourceProvisionReceipt struct {
	Version   int    `json:"version"`
	ID        string `json:"provision_id"`
	BackendID string `json:"backend_id"`
	NodeID    string `json:"node_id"`
	BootID    string `json:"boot_id"`
	Digest    string `json:"payload_sha256"`
}

var ErrSourceProvision = errors.New("source_provision_invalid")

func DecodeSourceProvision(raw []byte) (SourceProvision, error) {
	var p SourceProvision
	if len(raw) == 0 || len(raw) > 16384 {
		return p, ErrSourceProvision
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return SourceProvision{}, ErrSourceProvision
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(canonical, raw) || p.Version != 1 || p.IssuedAt.IsZero() || p.IssuedAt.Location() != time.UTC || len(p.Roots) != 3 {
		return SourceProvision{}, ErrSourceProvision
	}
	for _, id := range []string{p.ID, p.BackendID, p.NodeID, p.BootID} {
		if !validIssuerUUID(id) {
			return SourceProvision{}, ErrSourceProvision
		}
	}
	sources, ids := map[string]bool{}, map[string]bool{}
	first := p.Roots[0]
	for _, r := range p.Roots {
		if ValidateShareReference(r) != nil || r.ParentID != "" || r.ParentRevision != 0 || r.NodeID != p.NodeID || sources[r.SourceID] || ids[r.ID] || r.IssuerID != first.IssuerID || r.SignerEpoch != first.SignerEpoch || r.RuleID != first.RuleID || r.RuleRevision != first.RuleRevision || r.PathGeneration != first.PathGeneration || !p.IssuedAt.Before(r.ExpiresAt) {
			return SourceProvision{}, ErrSourceProvision
		}
		sources[r.SourceID], ids[r.ID] = true, true
	}
	return p, nil
}

// ApplySourceProvision commits all roots atomically under FULL SQLite. It does
// not install authorizations, change boot floors, or start an Engine listener.
func (s *Store) ApplySourceProvision(ctx context.Context, node, boot, backend string, raw []byte, now IssuerTime) (SourceProvisionReceipt, error) {
	p, e := DecodeSourceProvision(raw)
	if e != nil {
		return SourceProvisionReceipt{}, e
	}
	if s.owner != "agent" || p.NodeID != node || p.BootID != boot || p.BackendID != backend {
		return SourceProvisionReceipt{}, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return SourceProvisionReceipt{}, e
	}
	defer tx.Rollback()
	var previous, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT payload,receipt FROM node_source_provisions WHERE node_id=? AND provision_id=?`, node, p.ID).Scan(&previous, &receipt)
	if e == nil {
		if !bytes.Equal(previous, raw) {
			return SourceProvisionReceipt{}, ErrAuthorizationUpdateConflict
		}
		var out SourceProvisionReceipt
		if json.Unmarshal(receipt, &out) != nil {
			return SourceProvisionReceipt{}, ErrSourceProvision
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return SourceProvisionReceipt{}, e
	}
	if e = checkIssuerTime(now); e != nil {
		return SourceProvisionReceipt{}, e
	}
	if now.Earliest.Before(p.IssuedAt) {
		return SourceProvisionReceipt{}, ErrAuthorityTimeUncertain
	}
	for _, r := range p.Roots {
		if !now.Latest.Before(r.ExpiresAt) || !now.Latest.Before(r.HardDeadline) {
			return SourceProvisionReceipt{}, ErrAuthorityExpired
		}
	}
	for _, r := range p.Roots {
		if e = s.installShareSourceAnchorTx(ctx, tx, node, backend, r); e != nil {
			return SourceProvisionReceipt{}, e
		}
	}
	sum := sha256.Sum256(raw)
	out := SourceProvisionReceipt{Version: 1, ID: p.ID, BackendID: backend, NodeID: node, BootID: boot, Digest: hex.EncodeToString(sum[:])}
	encoded, e := json.Marshal(out)
	if e != nil {
		return SourceProvisionReceipt{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO node_source_provisions(node_id,provision_id,boot_id,backend_id,payload,receipt) VALUES(?,?,?,?,?,?)`, node, p.ID, boot, backend, raw, encoded); e != nil {
		return SourceProvisionReceipt{}, e
	}
	if e = tx.Commit(); e != nil {
		return SourceProvisionReceipt{}, e
	}
	return out, nil
}

func (s *Store) migrateSourceProvisions() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS node_source_provisions (
 node_id TEXT NOT NULL, provision_id TEXT NOT NULL, boot_id TEXT NOT NULL,
 backend_id TEXT NOT NULL, payload BLOB NOT NULL, receipt BLOB NOT NULL,
 PRIMARY KEY(node_id,provision_id)
 )`); e != nil {
		return e
	}
	return tx.Commit()
}
