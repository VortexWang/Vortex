package hub

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
)

// SameStoreRecoveryCommand is an explicit backend approval, not a heartbeat
// or proof of completed ledger replay. The consumer must pin the authenticated
// backend, actual Hub boot and complete S/R manifest before online admission.
type SameStoreRecoveryCommand struct {
	Version     int                       `json:"version"`
	ID          string                    `json:"recovery_id"`
	BackendID   string                    `json:"backend_id"`
	HubID       string                    `json:"hub_id"`
	FromBootID  string                    `json:"from_boot_id"`
	ToBootID    string                    `json:"to_boot_id"`
	IssuedAt    time.Time                 `json:"issued_at"`
	ExpiresAt   time.Time                 `json:"expires_at"`
	Handoff     store.HandoffRequest      `json:"handoff"`
	Allocations []SameStoreAllocationLink `json:"allocations"`
}

type SameStoreAllocationLink struct {
	AllocationID     string `json:"allocation_id"`
	PayloadSHA256    string `json:"allocation_sha256"`
	DelegationID     string `json:"delegation_id"`
	DelegationSHA256 string `json:"delegation_sha256"`
}

var ErrSameStoreRecovery = errors.New("invalid same-store Hub recovery")

func DecodeSameStoreRecovery(raw []byte) (SameStoreRecoveryCommand, error) {
	var c SameStoreRecoveryCommand
	if len(raw) == 0 || len(raw) > 1<<20 {
		return c, ErrSameStoreRecovery
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return SameStoreRecoveryCommand{}, ErrSameStoreRecovery
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(raw, canonical) || c.Validate() != nil {
		return SameStoreRecoveryCommand{}, ErrSameStoreRecovery
	}
	return c, nil
}

func (c SameStoreRecoveryCommand) Validate() error {
	for _, id := range []string{c.ID, c.BackendID, c.HubID, c.FromBootID, c.ToBootID} {
		u, err := uuid.Parse(id)
		if err != nil || u.String() != id {
			return ErrSameStoreRecovery
		}
	}
	h := c.Handoff
	if c.Version != 1 || c.IssuedAt.IsZero() || c.IssuedAt.Location() != time.UTC || c.ExpiresAt.Location() != time.UTC || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > 5*time.Minute || !h.Heartbeat.Equal(c.IssuedAt) || h.ID != c.ID || h.HubID != c.HubID || h.BackendID != c.BackendID || h.FromEpoch < 1 || h.FromEpoch >= math.MaxInt64 || h.ToEpoch != h.FromEpoch+1 || len(h.RevokeDelegations) != 0 || len(c.Allocations) == 0 || len(c.Allocations) > 4096 || len(h.FundedSuccessors) != len(c.Allocations) {
		return ErrSameStoreRecovery
	}
	dig := func(s string) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
	}
	ids := map[string]bool{}
	previous := ""
	for i, a := range c.Allocations {
		for _, id := range []string{a.AllocationID, a.DelegationID} {
			u, e := uuid.Parse(id)
			if e != nil || u.String() != id {
				return ErrSameStoreRecovery
			}
		}
		b := h.FundedSuccessors[i]
		if a.AllocationID <= previous || ids[a.DelegationID] || !dig(a.PayloadSHA256) || !dig(a.DelegationSHA256) || a.DelegationID != b.DelegationID || a.DelegationSHA256 != b.AllocationSHA256 || len(b.ShareReferences) != 3 {
			return ErrSameStoreRecovery
		}
		previous = a.AllocationID
		ids[a.DelegationID] = true
		sources, roots := map[string]bool{}, map[string]bool{}
		for _, r := range b.ShareReferences {
			if store.ValidateShareReference(r) != nil || r.ParentID != "" || r.ParentRevision != 0 || r.SignerEpoch != h.ToEpoch || r.IssuerID != c.HubID || sources[r.SourceID] || roots[r.ID] || !c.IssuedAt.Before(r.ExpiresAt) {
				return ErrSameStoreRecovery
			}
			sources[r.SourceID], roots[r.ID] = true, true
		}
	}
	return nil
}

// SameStoreRecoveryManifest is the exact immutable local FULL journal witness.
// Decoding checks command identity and conservation; PostgreSQL independently
// reconstructs every prefix hash and financial issue partition before accepting it.
type SameStoreRecoveryManifest struct {
	Request store.HandoffRequest
	Result  store.HandoffResult
}

func DecodeSameStoreManifest(raw []byte, c SameStoreRecoveryCommand) (SameStoreRecoveryManifest, error) {
	var m SameStoreRecoveryManifest
	if len(raw) == 0 || len(raw) > 1<<20 || c.Validate() != nil {
		return m, ErrSameStoreRecovery
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil {
		return SameStoreRecoveryManifest{}, ErrSameStoreRecovery
	}
	canonical, e := json.Marshal(m)
	a, _ := json.Marshal(m.Request)
	b, _ := json.Marshal(c.Handoff)
	inv := m.Result.FundedInventory
	if e != nil || !bytes.Equal(raw, canonical) || !bytes.Equal(a, b) || m.Result.SealedSequence < 0 || m.Result.SealedSequence >= math.MaxInt64 || inv == nil || inv.PrefixCount != m.Result.SealedSequence || len(inv.Delegations) != len(c.Allocations) || len(m.Result.Remaining) != len(c.Allocations) {
		return SameStoreRecoveryManifest{}, ErrSameStoreRecovery
	}
	validHash := func(s string) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
	}
	if !validHash(inv.PrefixSHA256) {
		return SameStoreRecoveryManifest{}, ErrSameStoreRecovery
	}
	for i, p := range inv.Delegations {
		a := c.Allocations[i]
		r, ok := m.Result.Remaining[a.DelegationID]
		if !ok || p.DelegationID != a.DelegationID || p.AllocationSHA256 != a.DelegationSHA256 || p.Raw < 1 || p.Issued < 0 || p.Issued > p.Raw || p.Remaining != p.Raw-p.Issued || uint64(p.Remaining) != r || p.IssueCount < 0 || p.IssueCount > p.Issued || !validHash(p.IssueSHA256) {
			return SameStoreRecoveryManifest{}, ErrSameStoreRecovery
		}
	}
	return m, nil
}
