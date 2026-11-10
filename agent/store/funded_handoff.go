package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
)

// FundedHandoffSuccessor is supplied only by the authenticated backend, never
// by a Node renewal. The immutable original allocation remains financial fact.
// Exactly three flat source roots advance one revision and one signer epoch;
// source, generation, capacity, policy and every original time bound stay fixed.
type FundedHandoffSuccessor struct {
	DelegationID string
	// Hash of the original issuer_delegations.payload (HubDelegation JSON),
	// not the distinct backend Allocation wire envelope. PG must link both.
	AllocationSHA256 string
	ShareReferences  []ShareReference
}

func fundedSuccessor(d HubDelegation, digest []byte, r HandoffRequest) (HubDelegation, error) {
	if !validIssuerUUID(r.BackendID) || len(d.ShareReferences) != 3 || d.SignerEpoch != r.FromEpoch || r.ToEpoch > math.MaxInt64 {
		return d, ErrAuthorityHandoffRequired
	}
	var binding *FundedHandoffSuccessor
	for i := range r.FundedSuccessors {
		if r.FundedSuccessors[i].DelegationID == d.ID {
			if binding != nil {
				return d, ErrAuthorizationUpdateConflict
			}
			binding = &r.FundedSuccessors[i]
		}
	}
	if binding == nil || binding.AllocationSHA256 != hex.EncodeToString(digest) || len(binding.ShareReferences) != 3 {
		return d, ErrAuthorityHandoffRequired
	}
	for i, old := range d.ShareReferences {
		if old.ParentID != "" || old.ParentRevision != 0 || old.SignerEpoch != r.FromEpoch || old.Revision >= math.MaxInt64 {
			return d, ErrAuthorizationDenied
		}
		expected := old
		expected.Revision++
		expected.SignerEpoch = r.ToEpoch
		a, _ := json.Marshal(expected)
		b, _ := json.Marshal(binding.ShareReferences[i])
		if !bytes.Equal(a, b) {
			return d, ErrAuthorizationDenied
		}
	}
	d.SignerEpoch = r.ToEpoch
	d.ShareReferences = append([]ShareReference(nil), binding.ShareReferences...)
	if _, err := delegationShareLeaves(d); err != nil {
		return d, err
	}
	return d, nil
}

// effectiveHubDelegationTx follows retained, hash-checked S/R attestations. It
// never rewrites the allocation payload or infers new rights from an epoch
// column. Spending and lease refresh additionally require the finite FULL ACK
// barrier to have completed. Sealing may still close issuance during recovery.
func effectiveHubDelegationTx(ctx context.Context, tx storeTransaction, d HubDelegation, digest []byte, owner string, spend bool) (HubDelegation, error) {
	target, err := strconv.ParseUint(owner, 10, 63)
	if err != nil || target < d.SignerEpoch || target-d.SignerEpoch > 4096 {
		return d, ErrAuthorityHandoffRequired
	}
	if d.ShareReferences == nil {
		d.SignerEpoch = target // Historical unfunded protocol adapter only.
		return d, nil
	}
	for d.SignerEpoch < target {
		var raw, journal, hash []byte
		var completed int
		if err := tx.QueryRowContext(ctx, `SELECT h.request,l.payload,l.payload_sha256,r.completed FROM hub_handoffs h JOIN hub_ledger l ON l.kind='handoff' AND l.record_id=h.handoff_id JOIN hub_recoveries r ON r.handoff_id=h.handoff_id WHERE h.hub_id=? AND h.from_epoch=?`, d.HubID, strconv.FormatUint(d.SignerEpoch, 10)).Scan(&raw, &journal, &hash, &completed); err != nil {
			return d, ErrAuthorityHandoffRequired
		}
		sum := sha256.Sum256(journal)
		var record struct {
			Request HandoffRequest
			Result  HandoffResult
		}
		if !bytes.Equal(sum[:], hash) || json.Unmarshal(journal, &record) != nil {
			return d, ErrAuthorizationDenied
		}
		canonical, _ := json.Marshal(record.Request)
		if !bytes.Equal(raw, canonical) || record.Request.HubID != d.HubID || record.Request.FromEpoch != d.SignerEpoch || record.Request.ToEpoch != d.SignerEpoch+1 {
			return d, ErrAuthorizationDenied
		}
		if _, ok := record.Result.Remaining[d.ID]; !ok {
			return d, ErrAuthorityHandoffRequired
		}
		if spend && completed != 1 {
			return d, ErrAuthorityHandoffRequired
		}
		d, err = fundedSuccessor(d, digest, record.Request)
		if err != nil {
			return d, err
		}
	}
	return d, nil
}

func (s *Store) installFundedHandoffTx(ctx context.Context, tx *sql.Tx, r HandoffRequest) error {
	if len(r.FundedSuccessors) > 4096 {
		return ErrAuthorizationDenied
	}
	bindings := map[string]bool{}
	for _, b := range r.FundedSuccessors {
		if !validIssuerUUID(b.DelegationID) || bindings[b.DelegationID] {
			return ErrAuthorizationDenied
		}
		bindings[b.DelegationID] = true
	}
	revoked := map[string]bool{}
	for _, id := range r.RevokeDelegations {
		if revoked[id] {
			return ErrAuthorizationDenied
		}
		revoked[id] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload,payload_sha256,signer_epoch FROM issuer_delegations WHERE hub_id=? AND state='open' AND signer_epoch=? ORDER BY delegation_id`, r.HubID, strconv.FormatUint(r.FromEpoch, 10))
	if err != nil {
		return err
	}
	type allocation struct {
		d      HubDelegation
		digest []byte
		owner  string
	}
	var allocations []allocation
	for rows.Next() {
		var raw []byte
		var a allocation
		if err = rows.Scan(&raw, &a.digest, &a.owner); err != nil {
			break
		}
		sum := sha256.Sum256(raw)
		if !bytes.Equal(sum[:], a.digest) || json.Unmarshal(raw, &a.d) != nil || a.d.HubID != r.HubID {
			err = ErrAuthorizationDenied
			break
		}
		allocations = append(allocations, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range allocations {
		if a.d.ShareReferences == nil || revoked[a.d.ID] {
			continue
		}
		d, err := effectiveHubDelegationTx(ctx, tx, a.d, a.digest, a.owner, true)
		if err != nil {
			return err
		}
		for _, root := range d.ShareReferences {
			if err := checkShareChainTx(ctx, tx, d.NodeID, d.HubID, root, true); err != nil {
				return err
			}
		}
		d, err = fundedSuccessor(d, a.digest, r)
		if err != nil {
			return err
		}
		delete(bindings, d.ID)
		for _, root := range d.ShareReferences {
			if err := s.installShareSourceAnchorTx(ctx, tx, d.NodeID, r.BackendID, root); err != nil {
				return err
			}
		}
		if err := persistShareReferencesTx(ctx, tx, AuthorizationUpdate{IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: d.SignerEpoch, PathGeneration: d.PathGeneration, ShareReferences: d.ShareReferences}); err != nil {
			return err
		}
	}
	if len(bindings) != 0 || len(r.FundedSuccessors) == 0 && r.BackendID != "" {
		return ErrAuthorizationDenied
	}
	return nil
}

// Retained S/R witnesses are append-only. Confirmation may change a journal
// row's confirmed flag, but it cannot rewrite the attested request or R.
func (s *Store) migrateFundedHandoffEvidence() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
CREATE TRIGGER IF NOT EXISTS immutable_hub_handoff_update BEFORE UPDATE ON hub_handoffs BEGIN SELECT RAISE(ABORT,'immutable handoff'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_handoff_delete BEFORE DELETE ON hub_handoffs BEGIN SELECT RAISE(ABORT,'immutable handoff'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_transfer_update BEFORE UPDATE ON hub_transfers BEGIN SELECT RAISE(ABORT,'immutable transfer'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_transfer_delete BEFORE DELETE ON hub_transfers BEGIN SELECT RAISE(ABORT,'immutable transfer'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_handoff_journal_update BEFORE UPDATE OF sequence,kind,record_id,payload,payload_sha256 ON hub_ledger WHEN OLD.kind='handoff' BEGIN SELECT RAISE(ABORT,'immutable handoff journal'); END;
CREATE TRIGGER IF NOT EXISTS immutable_hub_handoff_journal_delete BEFORE DELETE ON hub_ledger WHEN OLD.kind='handoff' BEGIN SELECT RAISE(ABORT,'immutable handoff journal'); END;
INSERT INTO schema_meta(key,value) VALUES('funded_handoff_evidence_version','1') ON CONFLICT(key) DO NOTHING;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
