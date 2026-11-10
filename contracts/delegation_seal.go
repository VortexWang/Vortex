package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
)

var ErrDelegationSeal = errors.New("funded_delegation_seal_invalid")

// IssuedPartition is a complete immutable issue inventory, ordered by issue ID.
// The digest binds individual payloads and amounts, not just their total.
type IssuedPartition struct {
	ID     string `json:"issue_id"`
	Digest string `json:"payload_sha256"`
	Raw    int64  `json:"raw_bytes"`
}

func PartitionDigest(partitions []IssuedPartition) (string, int64, error) {
	if partitions == nil {
		partitions = []IssuedPartition{}
	}
	var total int64
	for i, p := range partitions {
		digest, e := hex.DecodeString(p.Digest)
		if !closureUUID(p.ID) || e != nil || len(digest) != 32 || hex.EncodeToString(digest) != p.Digest || p.Raw < 1 || p.Raw > math.MaxInt64-total || i > 0 && partitions[i-1].ID >= p.ID {
			return "", 0, ErrDelegationSeal
		}
		total += p.Raw
	}
	raw, e := json.Marshal(partitions)
	if e != nil {
		return "", 0, e
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), total, nil
}

// DelegationSeal proves permanent end of issuance. It authorizes returning only
// the never-issued remainder; every issued child's potential debt stays held.
type DelegationSeal struct {
	Version      int    `json:"version"`
	ID           string `json:"seal_id"`
	DelegationID string `json:"delegation_id"`
	HubID        string `json:"hub_id"`
	HubBootID    string `json:"hub_boot_id"`
	NodeID       string `json:"node_id"`
	BootID       string `json:"boot_id"`
	SignerEpoch  int64  `json:"signer_epoch"`
	Raw          int64  `json:"raw_bytes"`
	Issued       int64  `json:"issued_raw_bytes"`
	Unissued     int64  `json:"unissued_raw_bytes"`
	IssueCount   int64  `json:"issue_count"`
	IssueDigest  string `json:"issues_sha256"`
}

func (s DelegationSeal) Validate() error {
	if s.Version != 1 || s.SignerEpoch < 1 || s.Raw < 1 || s.Issued < 0 || s.Issued > s.Raw || s.Unissued != s.Raw-s.Issued || s.IssueCount < 0 || s.IssueCount > s.Issued || (s.IssueCount == 0) != (s.Issued == 0) {
		return ErrDelegationSeal
	}
	for _, id := range []string{s.ID, s.DelegationID, s.HubID, s.HubBootID, s.NodeID, s.BootID} {
		if !closureUUID(id) {
			return ErrDelegationSeal
		}
	}
	digest, e := hex.DecodeString(s.IssueDigest)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != s.IssueDigest {
		return ErrDelegationSeal
	}
	return nil
}
func DecodeDelegationSeal(raw []byte) (DelegationSeal, error) {
	var s DelegationSeal
	if len(raw) == 0 || len(raw) > 2048 || json.Unmarshal(raw, &s) != nil || s.Validate() != nil {
		return s, ErrDelegationSeal
	}
	canonical, e := json.Marshal(s)
	if e != nil || !bytes.Equal(canonical, raw) {
		return DelegationSeal{}, ErrDelegationSeal
	}
	return s, nil
}
