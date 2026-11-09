package contracts

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/google/uuid"
)

var ErrShareRenewalClosure = errors.New("share_renewal_closure_invalid")

// ShareRenewalClosure closes a complete, immutable page of old delegations.
// Independent rate and connection generations are never merged. An empty page
// is an explicit closure statement, not an omitted command.
type ShareRenewalClosure struct {
	Version             int      `json:"version"`
	ID                  string   `json:"command_id"`
	BackendID           string   `json:"backend_id"`
	TenantID            string   `json:"tenant_id"`
	HandoffID           string   `json:"handoff_id"`
	HubID               string   `json:"hub_id"`
	HubBootID           string   `json:"hub_boot_id"`
	NodeID              string   `json:"node_id"`
	BootID              string   `json:"boot_id"`
	Kind                string   `json:"kind"`
	Generation          int64    `json:"generation"`
	SuccessorGeneration int64    `json:"successor_generation"`
	SignerEpoch         int64    `json:"signer_epoch"`
	Part                int32    `json:"part"`
	Parts               int32    `json:"parts"`
	DelegationIDs       []string `json:"delegation_ids"`
}

type ShareRenewalClosureReceipt struct {
	Version       int    `json:"version"`
	ID            string `json:"command_id"`
	CommandDigest string `json:"command_sha256"`
}

func closureUUID(id string) bool { v, e := uuid.Parse(id); return e == nil && v.String() == id }
func (c ShareRenewalClosure) Validate() error {
	if c.Version != 1 || c.Kind != "rate" && c.Kind != "connections" || c.Generation < 1 || c.Generation == math.MaxInt64 || c.SuccessorGeneration != c.Generation+1 || c.SignerEpoch < 1 || c.Part < 1 || c.Parts < c.Part || c.DelegationIDs == nil || len(c.DelegationIDs) > 64 || len(c.DelegationIDs) == 0 && c.Parts != 1 || c.Part < c.Parts && len(c.DelegationIDs) != 64 {
		return ErrShareRenewalClosure
	}
	for _, id := range []string{c.ID, c.BackendID, c.TenantID, c.HandoffID, c.HubID, c.HubBootID, c.NodeID, c.BootID} {
		if !closureUUID(id) {
			return ErrShareRenewalClosure
		}
	}
	if !slices.IsSorted(c.DelegationIDs) {
		return ErrShareRenewalClosure
	}
	for i, id := range c.DelegationIDs {
		if !closureUUID(id) || i > 0 && id == c.DelegationIDs[i-1] {
			return ErrShareRenewalClosure
		}
	}
	return nil
}
func DecodeShareRenewalClosure(raw []byte) (ShareRenewalClosure, error) {
	var c ShareRenewalClosure
	if len(raw) == 0 || len(raw) > 16384 || json.Unmarshal(raw, &c) != nil || c.Validate() != nil {
		return c, ErrShareRenewalClosure
	}
	canonical, e := json.Marshal(c)
	if e != nil || !bytes.Equal(canonical, raw) {
		return ShareRenewalClosure{}, ErrShareRenewalClosure
	}
	return c, nil
}
func DecodeShareRenewalClosureReceipt(raw []byte) (ShareRenewalClosureReceipt, error) {
	var r ShareRenewalClosureReceipt
	if len(raw) == 0 || len(raw) > 512 || json.Unmarshal(raw, &r) != nil || r.Version != 1 || !closureUUID(r.ID) {
		return r, ErrShareRenewalClosure
	}
	digest, e := hex.DecodeString(r.CommandDigest)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != r.CommandDigest {
		return r, ErrShareRenewalClosure
	}
	canonical, e := json.Marshal(r)
	if e != nil || !bytes.Equal(canonical, raw) {
		return ShareRenewalClosureReceipt{}, ErrShareRenewalClosure
	}
	return r, nil
}
