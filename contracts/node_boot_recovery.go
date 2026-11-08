package contracts

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"
)

var ErrNodeBootRecovery = errors.New("node_boot_recovery_invalid")

// NodeBootRecovery retires two independently numbered aggregate heads. It
// creates no active share, grant, stop proof or financial final. The complete
// PostgreSQL authority proof and explicit approval precede authenticated send.
type NodeBootRecovery struct {
	Version              int            `json:"version"`
	ID                   string         `json:"recovery_id"`
	BackendID            string         `json:"backend_id"`
	TenantID             string         `json:"tenant_id"`
	NodeID               string         `json:"node_id"`
	FromBootID           string         `json:"from_boot_id"`
	ToBootID             string         `json:"to_boot_id"`
	MinimumFencing       int64          `json:"minimum_fencing_generation"`
	SafeAfter            time.Time      `json:"safe_after"`
	IssuedAt             time.Time      `json:"issued_at"`
	ExpiresAt            time.Time      `json:"expires_at"`
	AuthorityProofSHA256 string         `json:"authority_proof_sha256"`
	Previous             []ShareHandoff `json:"previous"`
}

type RetiredNodeShareHead struct {
	Kind       string `json:"kind"`
	Generation int64  `json:"generation"`
}
type NodeBootRecoveryReceipt struct {
	Version        int                    `json:"version"`
	ID             string                 `json:"recovery_id"`
	NodeID         string                 `json:"node_id"`
	BootID         string                 `json:"boot_id"`
	CommandSHA256  string                 `json:"command_sha256"`
	MinimumFencing int64                  `json:"minimum_fencing_generation"`
	Retired        []RetiredNodeShareHead `json:"retired"`
}

func (c NodeBootRecovery) Validate() error {
	for _, id := range []string{c.ID, c.BackendID, c.TenantID, c.NodeID, c.FromBootID, c.ToBootID} {
		if !shareUUID(id) {
			return ErrNodeBootRecovery
		}
	}
	digest, e := hex.DecodeString(c.AuthorityProofSHA256)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != c.AuthorityProofSHA256 || c.Version != 1 || c.FromBootID == c.ToBootID || len(c.Previous) != 2 || c.MinimumFencing < 1 {
		return ErrNodeBootRecovery
	}
	for _, t := range []time.Time{c.SafeAfter, c.IssuedAt, c.ExpiresAt} {
		if t.IsZero() || t.Location() != time.UTC {
			return ErrNodeBootRecovery
		}
	}
	if c.IssuedAt.Before(c.SafeAfter) || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.After(c.IssuedAt.Add(5*time.Minute)) {
		return ErrNodeBootRecovery
	}
	for i, p := range c.Previous {
		if p.Validate() != nil || p.Kind != []string{"rate", "connections"}[i] || p.TenantID != c.TenantID || p.NodeID != c.NodeID || p.BootID != c.FromBootID || p.Generation == math.MaxInt64 || p.FencingGeneration >= c.MinimumFencing {
			return ErrNodeBootRecovery
		}
		for _, end := range []time.Time{p.OwnershipUntil, p.PolicyUntil, p.AutonomyUntil} {
			if c.SafeAfter.Before(end) {
				return ErrNodeBootRecovery
			}
		}
	}
	return nil
}

func DecodeNodeBootRecovery(raw []byte) (NodeBootRecovery, error) {
	var c NodeBootRecovery
	if len(raw) == 0 || len(raw) > 64<<10 {
		return c, ErrNodeBootRecovery
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || c.Validate() != nil {
		return NodeBootRecovery{}, ErrNodeBootRecovery
	}
	canonical, e := json.Marshal(c)
	if e != nil || !bytes.Equal(canonical, raw) {
		return NodeBootRecovery{}, ErrNodeBootRecovery
	}
	return c, nil
}
