package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// RecoveryAuthorization materializes the immutable control outbox after S/R
// commit. It includes revocations even when every delegation was revoked and
// no successor grant can be issued. Delivery/retry uses the existing mTLS stream.
func (s *Store) RecoveryAuthorization(ctx context.Context, handoff, node, boot string, generation uint64, now IssuerTime) (IssuedAuthorization, error) {
	var out IssuedAuthorization
	if s.owner != "hub" || !validIssuerUUID(node) || !validIssuerUUID(boot) || generation == 0 {
		return out, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM hub_recovery_updates WHERE handoff_id=? AND node_id=? AND boot_id=? AND path_generation=?`, handoff, node, boot, strconv.FormatUint(generation, 10)).Scan(&out.Payload, &out.Digest)
	if err == nil {
		sum := sha256.Sum256(out.Payload)
		if !bytes.Equal(sum[:], out.Digest) {
			return out, ErrAuthorizationDenied
		}
		out.Generation = generation
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var raw []byte
	var sealed int64
	var recordHash []byte
	if err = tx.QueryRowContext(ctx, `SELECT h.request,h.sealed_sequence,l.payload_sha256 FROM hub_handoffs h JOIN hub_ledger l ON l.kind='handoff' AND l.record_id=h.handoff_id WHERE h.handoff_id=?`, handoff).Scan(&raw, &sealed, &recordHash); err != nil {
		return out, err
	}
	var request HandoffRequest
	if err = json.Unmarshal(raw, &request); err != nil {
		return out, err
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT signer_epoch FROM issuer_epochs WHERE hub_id=?`, request.HubID).Scan(&current); err != nil {
		return out, err
	}
	if current != strconv.FormatUint(request.ToEpoch, 10) {
		return out, ErrSignerEpochClosed
	}
	deadline := deadlineFromTx(ctx, tx)
	if deadline.IsZero() || now.Earliest.Before(request.Heartbeat) || !now.Latest.Before(deadline) {
		return out, ErrAuthorityExpired
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.payload,d.state,COALESCE(t.remaining_bytes,''),d.payload_sha256,d.signer_epoch FROM issuer_delegations d LEFT JOIN hub_transfers t ON t.delegation_id=d.delegation_id AND t.handoff_id=? WHERE d.hub_id=? AND json_extract(CAST(d.payload AS TEXT),'$.NodeID')=? AND json_extract(CAST(d.payload AS TEXT),'$.BootID')=? AND CAST(json_extract(CAST(d.payload AS TEXT),'$.PathGeneration') AS INTEGER)=? AND EXISTS(SELECT 1 FROM issuer_issues i WHERE i.delegation_id=d.delegation_id) ORDER BY d.delegation_id`, handoff, request.HubID, node, boot, generation)
	if err != nil {
		return out, err
	}
	type entry struct {
		d                HubDelegation
		state, remaining string
		digest           []byte
		owner            string
	}
	var entries []entry
	for rows.Next() {
		var raw []byte
		var e entry
		if err = rows.Scan(&raw, &e.state, &e.remaining, &e.digest, &e.owner); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal(raw, &e.d); err != nil {
			rows.Close()
			return out, err
		}
		if e.d.NodeID == node && e.d.BootID == boot && e.d.PathGeneration == generation {
			sum := sha256.Sum256(raw)
			if !bytes.Equal(sum[:], e.digest) {
				rows.Close()
				return out, ErrAuthorizationDenied
			}
			entries = append(entries, e)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	revoked := []string{}
	proofs := []EpochHandoff{}
	refs := []ShareReference{}
	floor := uint64(0)
	for _, e := range entries {
		if e.d.FencingFloor > floor {
			floor = e.d.FencingFloor
		}
		if e.state == "revoked" {
			issued, err := tx.QueryContext(ctx, `SELECT payload FROM issuer_issues WHERE delegation_id=? ORDER BY issue_id`, e.d.ID)
			if err != nil {
				return out, err
			}
			for issued.Next() {
				var p []byte
				if err = issued.Scan(&p); err != nil {
					issued.Close()
					return out, err
				}
				var payload struct {
					Authorizations []struct {
						ID string `json:"authorization_id"`
					} `json:"authorizations"`
				}
				if err = json.Unmarshal(p, &payload); err != nil {
					issued.Close()
					return out, err
				}
				for _, a := range payload.Authorizations {
					revoked = append(revoked, a.ID)
				}
			}
			err = issued.Err()
			issued.Close()
			if err != nil {
				return out, err
			}
		} else if e.remaining != "" {
			amount, err := strconv.ParseUint(e.remaining, 10, 64)
			if err != nil {
				return out, err
			}
			d, err := effectiveHubDelegationTx(ctx, tx, e.d, e.digest, e.owner, false)
			if err != nil {
				return out, err
			}
			refs = append(refs, d.ShareReferences...)
			proofs = append(proofs, delegationEpochProofs(d, EpochHandoff{TransferID: handoff, ParentID: d.ID, SourceID: d.SourceID, From: request.FromEpoch, To: request.ToEpoch, Remaining: amount, SealedSequence: sealed, RecordSHA256: hex.EncodeToString(recordHash)})...)
		}
	}
	if floor == 0 || len(revoked) > 4096 || len(proofs) > 4096 || len(refs) > 4096 || len(revoked)+len(proofs) == 0 {
		return out, ErrAuthorizationDenied
	}
	// No grants are minted here; future unique issues consume the transferred R.
	// One handoff can cover multiple path scopes in the same Node database.
	// The outbox key includes that scope, so its wire idempotency ID must too.
	// Already persisted payloads still replay above without being rewritten.
	idHash := sha256.Sum256([]byte("vortex/runtime-recovery-update/v1/" + handoff + "/" + node + "/" + boot + "/" + strconv.FormatUint(generation, 10)))
	id := idHash[:16]
	id[6] = id[6]&0x0f | 0x50
	id[8] = id[8]&0x3f | 0x80
	updateID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	payload := map[string]any{"schema_version": "1.1.0", "update_id": updateID, "node_id": node, "boot_id": boot, "signer_epoch": request.ToEpoch, "path_generation": generation, "fencing_generation_floor": floor, "issued_at": now.Latest, "backend_heartbeat_at": request.Heartbeat, "autonomy_deadline": deadline, "authorizations": []any{}, "revocations": revoked, "epoch_handoffs": proofs, "restrictions": map[string]bool{"existing_rules_only": true, "existing_shares_only": true, "existing_delegated_balances_only": true, "new_rules": false, "new_ports": false, "dns_changes": false, "registrations": false, "cross_node_rebalance": false}}
	if len(refs) > 0 {
		payload["schema_version"], payload["share_references"], payload["policy_transitions"] = "1.2.0", refs, []any{}
	}
	out.Payload, err = json.Marshal(payload)
	if err != nil {
		return out, err
	}
	if len(out.Payload) > 1<<20 {
		return out, ErrAuthorizationDenied
	}
	sum := sha256.Sum256(out.Payload)
	out.Digest = sum[:]
	out.Generation = generation
	if _, err = tx.ExecContext(ctx, `INSERT INTO hub_recovery_updates(handoff_id,node_id,boot_id,path_generation,payload,payload_sha256) VALUES(?,?,?,?,?,?)`, handoff, node, boot, strconv.FormatUint(generation, 10), out.Payload, out.Digest); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
