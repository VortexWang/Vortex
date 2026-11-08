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

// HubRecoveryAdmission is supplied only over the independently authenticated
// backend admission channel. Retention receipts and ordinary heartbeats cannot
// construct this capability. The digest identifies a committed PG admission;
// the complete local immutable S/R manifest independently binds its finite ACK.
type HubRecoveryAdmission struct {
	Version                                       int
	RecoveryID, BackendID, HubID, BootID          string
	FromEpoch, ToEpoch                            uint64
	SealedSequence                                int64
	AdmissionSHA256, ManifestSHA256, PrefixSHA256 string
	ObservedAt, AutonomyDeadline                  time.Time
}

func DecodeHubRecoveryAdmission(raw []byte) (HubRecoveryAdmission, error) {
	var p HubRecoveryAdmission
	if len(raw) == 0 || len(raw) > 16<<10 {
		return p, ErrAuthorizationDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, ErrAuthorizationDenied
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(canonical, raw) || p.Version != 1 || p.FromEpoch == 0 || p.FromEpoch >= math.MaxInt64 || p.ToEpoch != p.FromEpoch+1 || p.SealedSequence < 0 || p.SealedSequence == math.MaxInt64 || p.ObservedAt.IsZero() || p.ObservedAt.Location() != time.UTC || p.AutonomyDeadline.Location() != time.UTC || !p.AutonomyDeadline.After(p.ObservedAt) || p.AutonomyDeadline.After(p.ObservedAt.Add(12*time.Hour)) {
		return p, ErrAuthorizationDenied
	}
	for _, id := range []string{p.RecoveryID, p.BackendID, p.HubID, p.BootID} {
		if !validIssuerUUID(id) {
			return p, ErrAuthorizationDenied
		}
	}
	for _, digest := range []string{p.AdmissionSHA256, p.ManifestSHA256, p.PrefixSHA256} {
		b, e := hex.DecodeString(digest)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != digest {
			return p, ErrAuthorizationDenied
		}
	}
	return p, nil
}

func sameRecoveryAdmission(a, b HubRecoveryAdmission) bool {
	// Fresh delivery time is not a new admission. The first FULL payload stays
	// immutable; replay cannot retime liveness, rights or the original deadline.
	a.ObservedAt, a.AutonomyDeadline = time.Time{}, time.Time{}
	b.ObservedAt, b.AutonomyDeadline = time.Time{}, time.Time{}
	return a == b
}

// ApplyHubRecoveryAdmission atomically verifies the entire local prefix, ACKs
// only that finite barrier and resumes the approved open epoch. It never settles
// usage, edits an allocation/transfer, extends original deadlines or adds funds.
// A fresh authenticated admission may refresh recovering liveness after the
// ordinary heartbeat grace; this path cannot revive offline/expired/closed state.
func (s *Store) ApplyHubRecoveryAdmission(ctx context.Context, backend, hub, boot string, raw []byte, now IssuerTime) ([]byte, error) {
	p, e := DecodeHubRecoveryAdmission(raw)
	if e != nil || s.owner != "hub" || p.BackendID != backend || p.HubID != hub || p.BootID != boot {
		return nil, ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var prior, digest []byte
	e = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM hub_recovery_admissions WHERE recovery_id=?`, p.RecoveryID).Scan(&prior, &digest)
	if e == nil {
		old, e := DecodeHubRecoveryAdmission(prior)
		h := sha256.Sum256(prior)
		if e != nil || !bytes.Equal(h[:], digest) || !sameRecoveryAdmission(old, p) {
			return nil, ErrAuthorizationUpdateConflict
		}
		return prior, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e = checkIssuerTime(now); e != nil {
		return nil, e
	}
	if now.Earliest.Before(p.ObservedAt) || !now.Latest.Before(p.ObservedAt.Add(5*time.Second)) || !now.Latest.Before(p.AutonomyDeadline) {
		return nil, ErrAuthorityTimeUncertain
	}
	mode, e := stateValue(ctx, tx, "authority_mode")
	if e != nil || mode != AutonomyRecovering {
		return nil, ErrAuthorityHandoffRequired
	}
	current, e := stateValue(ctx, tx, "recovery_handoff_id")
	if e != nil || current != p.RecoveryID {
		return nil, ErrAuthorityHandoffRequired
	}
	open, e := stateValue(ctx, tx, "signer_epoch_open")
	if e != nil || open != "true" {
		return nil, ErrSignerEpochClosed
	}
	var target int64
	var epochState string
	var manifest, manifestHash, handoff []byte
	if e = tx.QueryRowContext(ctx, `SELECT r.target_sequence,e.state,l.payload,l.payload_sha256,h.request FROM hub_recoveries r JOIN hub_handoffs h ON h.handoff_id=r.handoff_id JOIN issuer_epochs e ON e.hub_id=h.hub_id AND e.signer_epoch=h.to_epoch JOIN hub_ledger l ON l.kind='handoff' AND l.record_id=h.handoff_id WHERE r.handoff_id=? AND r.completed=0 AND h.hub_id=? AND h.from_epoch=? AND h.to_epoch=?`, p.RecoveryID, hub, strconv.FormatUint(p.FromEpoch, 10), strconv.FormatUint(p.ToEpoch, 10)).Scan(&target, &epochState, &manifest, &manifestHash, &handoff); e != nil {
		return nil, e
	}
	if epochState != "open" || target != p.SealedSequence+1 {
		return nil, ErrAuthorityHandoffRequired
	}
	mh := sha256.Sum256(manifest)
	var m struct {
		Request HandoffRequest
		Result  HandoffResult
	}
	if !bytes.Equal(mh[:], manifestHash) || hex.EncodeToString(mh[:]) != p.ManifestSHA256 || json.Unmarshal(manifest, &m) != nil {
		return nil, ErrUsageBatchConflict
	}
	canonical, _ := json.Marshal(m)
	request, _ := json.Marshal(m.Request)
	if !bytes.Equal(canonical, manifest) || !bytes.Equal(request, handoff) || m.Request.ID != p.RecoveryID || m.Request.BackendID != backend || m.Request.HubID != hub || m.Request.FromEpoch != p.FromEpoch || m.Request.ToEpoch != p.ToEpoch || m.Result.SealedSequence != p.SealedSequence || m.Result.FundedInventory == nil || m.Result.FundedInventory.PrefixCount != p.SealedSequence || m.Result.FundedInventory.PrefixSHA256 != p.PrefixSHA256 {
		return nil, ErrUsageBatchConflict
	}
	deadline := deadlineFromTx(ctx, tx)
	previous, e := stateValue(ctx, tx, "backend_heartbeat")
	if e != nil {
		return nil, e
	}
	hb, e := time.Parse(time.RFC3339Nano, previous)
	if e != nil || p.ObservedAt.Before(hb) || deadline.IsZero() || !now.Latest.Before(deadline) {
		return nil, ErrAuthorityExpired
	}
	if p.AutonomyDeadline.Before(deadline) {
		deadline = p.AutonomyDeadline
	}
	// Read every row, including already confirmed rows. Never ACK by a claimed
	// checkpoint, a single handoff hash or a possibly rolled-back row count.
	rows, e := tx.QueryContext(ctx, `SELECT sequence,kind,record_id,payload,payload_sha256 FROM hub_ledger WHERE sequence<=? ORDER BY sequence`, target)
	if e != nil {
		return nil, e
	}
	count := int64(0)
	prefix := sha256.New()
	enc := json.NewEncoder(prefix)
	for rows.Next() {
		var sequence int64
		var kind, id string
		var payload, hash []byte
		if e = rows.Scan(&sequence, &kind, &id, &payload, &hash); e != nil {
			break
		}
		h := sha256.Sum256(payload)
		if sequence != count+1 || !bytes.Equal(h[:], hash) {
			e = ErrUsageBatchConflict
			break
		}
		if sequence == target {
			if kind != "handoff" || id != p.RecoveryID || !bytes.Equal(payload, manifest) {
				e = ErrUsageBatchConflict
				break
			}
		} else {
			e = enc.Encode(struct {
				Sequence         int64
				Kind, ID, SHA256 string
			}{sequence, kind, id, hex.EncodeToString(h[:])})
			if e != nil {
				break
			}
		}
		count++
	}
	rowErr := rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if rowErr != nil {
		return nil, rowErr
	}
	if count != target || hex.EncodeToString(prefix.Sum(nil)) != p.PrefixSHA256 {
		return nil, ErrUsageBatchConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE hub_ledger SET confirmed=1 WHERE sequence<=?`, target); e != nil {
		return nil, e
	}
	var checkpoint int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT MIN(sequence)-1 FROM hub_ledger WHERE confirmed=0),(SELECT COALESCE(MAX(sequence),0) FROM hub_ledger))`).Scan(&checkpoint); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE hub_recoveries SET completed=1 WHERE handoff_id=?`, p.RecoveryID); e != nil {
		return nil, e
	}
	for key, value := range map[string]string{"ledger_checkpoint": strconv.FormatInt(checkpoint, 10), "backend_heartbeat": p.ObservedAt.Format(time.RFC3339Nano), "autonomy_deadline": deadline.Format(time.RFC3339Nano), "authority_mode": AutonomyOnline} {
		if e = putState(ctx, tx, key, value, p.ObservedAt); e != nil {
			return nil, e
		}
	}
	if e = writeAutonomyTransition(ctx, tx, AutonomyRecovering, AutonomyOnline, p.ObservedAt, p.ObservedAt, deadline); e != nil {
		return nil, e
	}
	h := sha256.Sum256(raw)
	if _, e = tx.ExecContext(ctx, `INSERT INTO hub_recovery_admissions(recovery_id,payload,payload_sha256) VALUES(?,?,?)`, p.RecoveryID, raw, h[:]); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return append([]byte(nil), raw...), nil
}

func (s *Store) migrateHubRecoveryAdmissions() error {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`CREATE TABLE IF NOT EXISTS hub_recovery_admissions(recovery_id TEXT PRIMARY KEY REFERENCES hub_handoffs(handoff_id),payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL CHECK(length(payload_sha256)=32));
CREATE TRIGGER IF NOT EXISTS immutable_hub_recovery_admission_update BEFORE UPDATE ON hub_recovery_admissions BEGIN SELECT RAISE(ABORT,'immutable recovery admission'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_recovery_admission_delete BEFORE DELETE ON hub_recovery_admissions BEGIN SELECT RAISE(ABORT,'immutable recovery admission'); END;`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
