package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"vortex/contracts"
)

// FundedHandoffInventory commits every retained journal identity through S and
// all original child partitions. Neither a local/remote ACK nor expiry clears
// issued or unknown debt. PostgreSQL must independently validate this manifest.
type FundedHandoffInventory struct {
	PrefixCount  int64
	PrefixSHA256 string
	Delegations  []FundedHandoffPartition
}

type FundedHandoffPartition struct {
	DelegationID, AllocationSHA256 string
	Raw, Issued, Remaining         int64
	IssueCount                     int64
	IssueSHA256                    string
}

// PrefixSHA256 hashes newline-delimited canonical {Sequence,Kind,ID,SHA256}
// objects, in sequence order starting at1. Payload hashes are checked against
// actual retained bytes, including records already acknowledged before S.
func fundedHandoffInventoryTx(ctx context.Context, tx *sql.Tx, r HandoffRequest, result HandoffResult) (*FundedHandoffInventory, error) {
	out := &FundedHandoffInventory{Delegations: []FundedHandoffPartition{}}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,kind,record_id,payload,payload_sha256 FROM hub_ledger WHERE sequence<=? ORDER BY sequence`, result.SealedSequence)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	enc := json.NewEncoder(hash)
	for rows.Next() {
		var item struct {
			Sequence         int64
			Kind, ID, SHA256 string
		}
		var raw, digest []byte
		if err = rows.Scan(&item.Sequence, &item.Kind, &item.ID, &raw, &digest); err != nil {
			break
		}
		sum := sha256.Sum256(raw)
		if item.Sequence != out.PrefixCount+1 || !bytes.Equal(sum[:], digest) {
			err = ErrAuthorizationDenied
			break
		}
		item.SHA256 = hex.EncodeToString(digest)
		if err = enc.Encode(item); err != nil {
			break
		}
		out.PrefixCount++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if out.PrefixCount != result.SealedSequence {
		return nil, ErrAuthorizationDenied
	}
	out.PrefixSHA256 = hex.EncodeToString(hash.Sum(nil))
	for _, binding := range r.FundedSuccessors {
		var raw, digest []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM issuer_delegations WHERE delegation_id=?`, binding.DelegationID).Scan(&raw, &digest); err != nil {
			return nil, err
		}
		var d HubDelegation
		sum := sha256.Sum256(raw)
		if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &d) != nil {
			return nil, ErrAuthorizationDenied
		}
		left, ok := result.Remaining[d.ID]
		if !ok || left > d.RawBytes {
			return nil, ErrAuthorizationDenied
		}
		rows, err = tx.QueryContext(ctx, `SELECT i.issue_id,i.payload,i.payload_sha256,i.amount_bytes,l.sequence,l.payload_sha256 FROM issuer_issues i LEFT JOIN hub_ledger l ON l.kind='issue' AND l.record_id=i.issue_id WHERE i.delegation_id=? ORDER BY i.issue_id`, d.ID)
		if err != nil {
			return nil, err
		}
		inventory := []contracts.IssuedPartition{}
		for rows.Next() {
			var item contracts.IssuedPartition
			var amount string
			var payload, digest, ledgerDigest []byte
			var seq sql.NullInt64
			if err = rows.Scan(&item.ID, &payload, &digest, &amount, &seq, &ledgerDigest); err != nil {
				break
			}
			sum := sha256.Sum256(payload)
			if !seq.Valid || seq.Int64 < 1 || seq.Int64 > result.SealedSequence || !bytes.Equal(sum[:], digest) || !bytes.Equal(digest, ledgerDigest) {
				err = ErrAuthorizationDenied
				break
			}
			item.Raw, err = strconv.ParseInt(amount, 10, 64)
			if err != nil {
				break
			}
			if !fundedPartitionPayloadMatches(payload, d, item.Raw, r.FromEpoch) {
				err = ErrAuthorizationDenied
				break
			}
			item.Digest = hex.EncodeToString(digest)
			inventory = append(inventory, item)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		digestText, issued, err := contracts.PartitionDigest(inventory)
		if err != nil || uint64(issued) > d.RawBytes || left != d.RawBytes-uint64(issued) {
			return nil, ErrAuthorizationDenied
		}
		out.Delegations = append(out.Delegations, FundedHandoffPartition{DelegationID: d.ID, AllocationSHA256: binding.AllocationSHA256, Raw: int64(d.RawBytes), Issued: issued, Remaining: int64(left), IssueCount: int64(len(inventory)), IssueSHA256: digestText})
	}
	return out, nil
}

func fundedPartitionPayloadMatches(raw []byte, d HubDelegation, amount int64, latestEpoch uint64) bool {
	var p struct {
		NodeID         string `json:"node_id"`
		BootID         string `json:"boot_id"`
		Epoch          uint64 `json:"signer_epoch"`
		Path           uint64 `json:"path_generation"`
		Authorizations []struct {
			Kind     string  `json:"kind"`
			Source   string  `json:"source_id"`
			Parent   string  `json:"parent_authorization_id"`
			Snapshot string  `json:"billing_snapshot_id"`
			Amount   *uint64 `json:"amount_bytes"`
		} `json:"authorizations"`
	}
	if json.Unmarshal(raw, &p) != nil || amount < 1 || p.NodeID != d.NodeID || p.BootID != d.BootID || p.Path != d.PathGeneration || p.Epoch < d.SignerEpoch || p.Epoch > latestEpoch {
		return false
	}
	credits := 0
	for _, a := range p.Authorizations {
		if a.Kind != "credit_grant" {
			continue
		}
		credits++
		if a.Source != d.SourceID || a.Parent != d.ID || a.Snapshot != d.BillingSnapshotID || a.Amount == nil || *a.Amount != uint64(amount) {
			return false
		}
	}
	return credits == 1
}
