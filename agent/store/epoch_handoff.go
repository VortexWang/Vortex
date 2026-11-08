package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

type EpochHandoff struct {
	TransferID     string `json:"transfer_id"`
	ParentID       string `json:"parent_authorization_id"`
	SourceID       string `json:"source_id"`
	From           uint64 `json:"from_epoch"`
	To             uint64 `json:"to_epoch"`
	Remaining      uint64 `json:"remaining_bytes"`
	SealedSequence int64  `json:"sealed_sequence"`
	RecordSHA256   string `json:"record_sha256"`
}

func persistEpochHandoffTx(ctx context.Context, tx *sql.Tx, u AuthorizationUpdate, h EpochHandoff) error {
	hash, e := hex.DecodeString(h.RecordSHA256)
	if u.IssuerID == "" || !validIssuerUUID(h.TransferID) || h.ParentID == "" || h.SourceID == "" || h.From == 0 || h.From == math.MaxUint64 || h.To != h.From+1 || h.To != u.SignerEpoch || h.Remaining > math.MaxInt64 || h.SealedSequence < 0 || e != nil || len(hash) != 32 {
		return ErrAuthorityHandoffRequired
	}
	raw, e := json.Marshal(h)
	if e != nil {
		return e
	}
	var previous []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM authorization_handoffs WHERE node_id=? AND source_id=? AND from_epoch=?`, u.NodeID, h.SourceID, strconv.FormatUint(h.From, 10)).Scan(&previous)
	if e == nil {
		if !bytes.Equal(previous, raw) {
			return ErrAuthorizationUpdateConflict
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var rootID string
	e = tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_source_owners WHERE node_id=? AND source_id=?`, u.NodeID, h.SourceID).Scan(&rootID)
	if errors.Is(e, sql.ErrNoRows) && h.Remaining == 0 {
		// Nonfinancial sources still require an original locally known family.
		e = tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_records WHERE node_id=? AND source_id=? AND parent_authorization_id=? AND issuer_id=? AND boot_id=? AND kind IN ('runtime_lease','share_lease') AND state='active' ORDER BY length(signer_epoch),signer_epoch,rowid LIMIT 1`, u.NodeID, h.SourceID, h.ParentID, u.IssuerID, u.BootID).Scan(&rootID)
	}
	if e != nil {
		return ErrAuthorityHandoffRequired
	}
	root, state, e := readAuthorizationTx(ctx, tx, u.NodeID, rootID)
	if e != nil {
		return e
	}
	if state != "active" || root.ParentAuthorizationID != h.ParentID || root.IssuerID != u.IssuerID || root.BootID != u.BootID || root.PathGeneration != u.PathGeneration {
		return ErrAuthorityHandoffRequired
	}
	latest, e := sourceEpochTx(ctx, tx, u.NodeID, h.SourceID, root.SignerEpoch)
	if e != nil {
		return e
	}
	if latest != h.From {
		return ErrAuthorityHandoffRequired
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO authorization_handoffs(node_id,source_id,from_epoch,to_epoch,transfer_id,payload,remaining_bytes) VALUES(?,?,?,?,?,?,?)`, u.NodeID, h.SourceID, strconv.FormatUint(h.From, 10), strconv.FormatUint(h.To, 10), h.TransferID, raw, strconv.FormatUint(h.Remaining, 10))
	return e
}

func sourceEpochTx(ctx context.Context, tx *sql.Tx, node, source string, root uint64) (uint64, error) {
	rows, e := tx.QueryContext(ctx, `SELECT from_epoch,to_epoch FROM authorization_handoffs WHERE node_id=? AND source_id=? ORDER BY length(to_epoch),to_epoch`, node, source)
	if e != nil {
		return 0, e
	}
	defer rows.Close()
	current := root
	for rows.Next() {
		var a, b string
		if e = rows.Scan(&a, &b); e != nil {
			return 0, e
		}
		from, e := strconv.ParseUint(a, 10, 64)
		if e != nil {
			return 0, e
		}
		to, e := strconv.ParseUint(b, 10, 64)
		if e != nil {
			return 0, e
		}
		if from != current || to != from+1 {
			return 0, ErrAuthorityHandoffRequired
		}
		current = to
	}
	return current, rows.Err()
}

func spendHandoffBudgetTx(ctx context.Context, tx *sql.Tx, node string, r AuthorizationRecord) error {
	var rootEpoch, rootState string
	if e := tx.QueryRowContext(ctx, `SELECT a.signer_epoch,a.state FROM authorization_source_owners o JOIN authorization_records a ON a.node_id=o.node_id AND a.authorization_id=o.authorization_id WHERE o.node_id=? AND o.source_id=?`, node, r.SourceID).Scan(&rootEpoch, &rootState); e != nil {
		return e
	}
	if rootState != "active" {
		return ErrAuthorizationDenied
	}
	root, e := strconv.ParseUint(rootEpoch, 10, 64)
	if e != nil {
		return e
	}
	current, e := sourceEpochTx(ctx, tx, node, r.SourceID, root)
	if e != nil {
		return e
	}
	if current != r.SignerEpoch {
		return ErrAuthorityHandoffRequired
	}
	if current == root {
		return nil
	}
	var remainingText string
	if e = tx.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_handoffs WHERE node_id=? AND source_id=? AND to_epoch=?`, node, r.SourceID, strconv.FormatUint(current, 10)).Scan(&remainingText); e != nil {
		return e
	}
	n, e := strconv.ParseUint(remainingText, 10, 64)
	if e != nil {
		return e
	}
	if r.AmountBytes > n {
		return ErrAuthorizationDenied
	}
	_, e = tx.ExecContext(ctx, `UPDATE authorization_handoffs SET remaining_bytes=? WHERE node_id=? AND source_id=? AND to_epoch=?`, strconv.FormatUint(n-r.AmountBytes, 10), node, r.SourceID, strconv.FormatUint(current, 10))
	return e
}

func resumeHandoffHeartbeatTx(ctx context.Context, tx *sql.Tx, u AuthorizationUpdate) error {
	oldText, e := stateValue(ctx, tx, "backend_heartbeat")
	if e != nil {
		return e
	}
	old, e := time.Parse(time.RFC3339Nano, oldText)
	if e != nil {
		return e
	}
	deadline := deadlineFromTx(ctx, tx)
	if deadline.IsZero() || !u.BackendHeartbeat.After(old) || !u.BackendHeartbeat.Before(deadline) || u.AutonomyDeadline.After(deadline) {
		return ErrAuthorityExpired
	}
	for key, v := range map[string]string{"backend_heartbeat": u.BackendHeartbeat.UTC().Format(time.RFC3339Nano), "autonomy_deadline": u.AutonomyDeadline.UTC().Format(time.RFC3339Nano), "authority_mode": AutonomyOnline, "signer_epoch_open": "true"} {
		if e = putState(ctx, tx, key, v, u.BackendHeartbeat); e != nil {
			return e
		}
	}
	return nil
}

// attachEpochHandoffTx emits the immutable attestation from the same store
// that transferred the unissued balance, never from a node-supplied request.
func attachEpochHandoffTx(ctx context.Context, tx *sql.Tx, d HubDelegation, result *IssuedAuthorization) error {
	var h EpochHandoff
	var remaining, from string
	var hash []byte
	err := tx.QueryRowContext(ctx, `SELECT h.handoff_id,h.from_epoch,t.remaining_bytes,h.sealed_sequence,l.payload_sha256 FROM hub_handoffs h JOIN hub_transfers t ON t.handoff_id=h.handoff_id JOIN hub_ledger l ON l.kind='handoff' AND l.record_id=h.handoff_id WHERE h.hub_id=? AND h.to_epoch=? AND t.delegation_id=?`, d.HubID, strconv.FormatUint(d.SignerEpoch, 10), d.ID).Scan(&h.TransferID, &from, &remaining, &h.SealedSequence, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	h.From, err = strconv.ParseUint(from, 10, 64)
	if err != nil {
		return err
	}
	h.Remaining, err = strconv.ParseUint(remaining, 10, 64)
	if err != nil {
		return err
	}
	h.To = d.SignerEpoch
	h.ParentID = d.ID
	h.SourceID = d.SourceID
	h.RecordSHA256 = hex.EncodeToString(hash)
	var payload map[string]any
	if err = json.Unmarshal(result.Payload, &payload); err != nil {
		return err
	}
	proofs := delegationEpochProofs(d, h)
	if d.ShareReferences == nil {
		payload["schema_version"] = "1.1.0"
	}
	payload["epoch_handoffs"] = proofs
	result.Payload, err = json.Marshal(payload)
	return err
}

func delegationEpochProofs(d HubDelegation, h EpochHandoff) []EpochHandoff {
	proofs := []EpochHandoff{h}
	if d.ShareReferences != nil {
		for _, source := range []string{d.RuntimeSourceID, d.ShareSourceID} {
			r := h
			r.SourceID, r.Remaining = source, 0
			proofs = append(proofs, r)
		}
	}
	return proofs
}
