package contracts

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
)

var ErrCreditClose = errors.New("credit_close_invalid")

// CreditCloseCommand carries original immutable Hub issue bytes, never fresh
// authority. The enclosing mTLS session must authenticate the pinned backend.
type CreditCloseCommand struct {
	Version    int    `json:"version"`
	ID         string `json:"command_id"`
	BackendID  string `json:"backend_id"`
	HubID      string `json:"hub_id"`
	NodeID     string `json:"node_id"`
	BootID     string `json:"boot_id"`
	GrantID    string `json:"grant_id"`
	Generation uint64 `json:"path_generation"`
	Original   []byte `json:"original_issue"`
}

func (c CreditCloseCommand) Validate() error {
	if c.Version != 1 || c.Generation == 0 || c.Generation > math.MaxInt64 || len(c.Original) == 0 || len(c.Original) > 512<<10 {
		return ErrCreditClose
	}
	for _, id := range []string{c.ID, c.BackendID, c.HubID, c.NodeID, c.BootID, c.GrantID} {
		if !closureUUID(id) {
			return ErrCreditClose
		}
	}
	return nil
}

func DecodeCreditCloseCommand(raw []byte) (CreditCloseCommand, error) {
	var c CreditCloseCommand
	if len(raw) == 0 || len(raw) > 768<<10 || json.Unmarshal(raw, &c) != nil || c.Validate() != nil {
		return c, ErrCreditClose
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(raw, canonical) {
		return CreditCloseCommand{}, ErrCreditClose
	}
	return c, nil
}

// Unsettled is only a conservative retry result. Final contains local durable
// evidence; it does not itself acknowledge PostgreSQL acceptance or release debt.
type CreditCloseReceipt struct {
	Version       int             `json:"version"`
	CommandID     string          `json:"command_id"`
	BackendID     string          `json:"backend_id"`
	NodeID        string          `json:"node_id"`
	BootID        string          `json:"boot_id"`
	GrantID       string          `json:"grant_id"`
	CommandSHA256 string          `json:"command_sha256"`
	State         string          `json:"state"`
	Final         json.RawMessage `json:"final,omitempty"`
}

func (r CreditCloseReceipt) Validate() error {
	if r.Version != 1 || r.State != "unsettled" && r.State != "final" || r.State == "final" && (len(r.Final) == 0 || len(r.Final) > 8192 || !json.Valid(r.Final)) || r.State == "unsettled" && len(r.Final) != 0 {
		return ErrCreditClose
	}
	for _, id := range []string{r.CommandID, r.BackendID, r.NodeID, r.BootID, r.GrantID} {
		if !closureUUID(id) {
			return ErrCreditClose
		}
	}
	hash, err := hex.DecodeString(r.CommandSHA256)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != r.CommandSHA256 {
		return ErrCreditClose
	}
	return nil
}

func DecodeCreditCloseReceipt(raw []byte) (CreditCloseReceipt, error) {
	var r CreditCloseReceipt
	if len(raw) == 0 || len(raw) > 16384 || json.Unmarshal(raw, &r) != nil || r.Validate() != nil {
		return r, ErrCreditClose
	}
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(raw, canonical) {
		return CreditCloseReceipt{}, ErrCreditClose
	}
	return r, nil
}
