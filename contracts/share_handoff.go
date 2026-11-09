package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var ErrShareHandoff = errors.New("share_handoff_invalid")

// ShareHandoff is a backend-authenticated capacity decision, never a traffic
// grant. Every generation belongs to one tenant/node/kind and one boot only.
// Query and CAS use identical payload bytes; phase belongs to the wire envelope.
type ShareHandoff struct {
	TenantID          string    `json:"tenant_id"`
	ID                string    `json:"handoff_id"`
	NodeID            string    `json:"node_id"`
	BootID            string    `json:"boot_id"`
	Kind              string    `json:"kind"`
	SignerID          string    `json:"signer_id"`
	SnapshotID        string    `json:"billing_snapshot_id"`
	RuleID            string    `json:"rule_id"`
	Generation        int64     `json:"generation"`
	Predecessor       int64     `json:"predecessor"`
	SignerEpoch       int64     `json:"signer_epoch"`
	PathGeneration    int64     `json:"path_generation"`
	FencingGeneration int64     `json:"fencing_generation"`
	RuleRevision      int64     `json:"rule_revision"`
	Amount            int64     `json:"amount"`
	BurstBytes        int64     `json:"burst_bytes"`
	ClosureDigest     string    `json:"closure_sha256"`
	IssuedAt          time.Time `json:"issued_at"`
	OwnershipUntil    time.Time `json:"ownership_until"`
	PolicyUntil       time.Time `json:"policy_until"`
	AutonomyUntil     time.Time `json:"autonomy_until"`
}

type ShareObservation struct {
	ReceiptID         string `json:"receipt_id"`
	HandoffID         string `json:"handoff_id"`
	NodeID            string `json:"node_id"`
	BootID            string `json:"boot_id"`
	Kind              string `json:"kind"`
	CommandDigest     string `json:"command_sha256"`
	State             string `json:"state"`
	Generation        int64  `json:"generation"`
	MinimumGeneration int64  `json:"minimum_generation"`
	ActiveGeneration  int64  `json:"active_generation"`
	FencingGeneration int64  `json:"fencing_generation"`
	OpenConnections   int64  `json:"open_connections"`
}

func SharePayloadDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func DecodeShareHandoff(raw []byte) (ShareHandoff, error) {
	var c ShareHandoff
	if len(raw) > 16384 {
		return c, ErrShareHandoff
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, ErrShareHandoff
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, ErrShareHandoff
	}
	return c, c.Validate()
}
func (c ShareHandoff) Validate() error {
	for _, id := range []string{c.TenantID, c.ID, c.NodeID, c.BootID, c.SignerID, c.SnapshotID, c.RuleID} {
		if !shareUUID(id) {
			return ErrShareHandoff
		}
	}
	if c.Generation < 1 || c.Predecessor != c.Generation-1 || c.SignerEpoch < 1 || c.PathGeneration < 1 || c.FencingGeneration < 1 || c.RuleRevision < 1 || c.Amount < 1 || c.BurstBytes < 0 {
		return ErrShareHandoff
	}
	if c.Kind == "rate" {
		if c.Amount > 1_000_000_000_000 || c.BurstBytes > c.Amount/8 {
			return ErrShareHandoff
		}
	} else if c.Kind == "connections" {
		if c.Amount > 1_000_000 || c.BurstBytes != 0 {
			return ErrShareHandoff
		}
	} else {
		return ErrShareHandoff
	}
	if c.Predecessor == 0 {
		if c.ClosureDigest != "" {
			return ErrShareHandoff
		}
	} else {
		v, e := hex.DecodeString(c.ClosureDigest)
		if e != nil || len(v) != 32 {
			return ErrShareHandoff
		}
	}
	for _, end := range []time.Time{c.OwnershipUntil, c.PolicyUntil, c.AutonomyUntil} {
		if c.IssuedAt.IsZero() || c.IssuedAt.Location() != time.UTC || !end.After(c.IssuedAt) || end.Location() != time.UTC {
			return ErrShareHandoff
		}
	}
	return nil
}
func shareUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (r ShareObservation) Validate() error {
	for _, id := range []string{r.HandoffID, r.NodeID, r.BootID} {
		if !shareUUID(id) {
			return ErrShareHandoff
		}
	}
	for _, x := range []struct {
		value string
		size  int
	}{{r.ReceiptID, 16}, {r.CommandDigest, 32}} {
		b, e := hex.DecodeString(x.value)
		if e != nil || len(b) != x.size || hex.EncodeToString(b) != x.value {
			return ErrShareHandoff
		}
	}
	if r.Kind != "rate" && r.Kind != "connections" || r.Generation < 1 || r.MinimumGeneration < 0 || r.ActiveGeneration < 0 || r.FencingGeneration < 0 || r.OpenConnections < 0 {
		return ErrShareHandoff
	}
	switch r.State {
	case "enforced", "unknown":
	case "draining":
		if r.Kind != "connections" {
			return ErrShareHandoff
		}
	default:
		return ErrShareHandoff
	}
	return nil
}
