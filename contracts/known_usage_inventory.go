package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

var ErrKnownUsageInventory = errors.New("known_usage_inventory_invalid")

// KnownUsageInventory binds original committed wire batches only. Returned
// local tickets never went on the wire and are deliberately excluded. Version
// zero is incomplete historical evidence, even when Count is zero.
type KnownUsageInventory struct {
	Version int    `json:"version"`
	Count   uint64 `json:"count"`
	SHA256  string `json:"sha256"`
}

type KnownUsageBatch struct {
	BatchID string
	SHA256  string
}

func (i KnownUsageInventory) Validate() error {
	sum, err := hex.DecodeString(i.SHA256)
	if i.Version != 1 || err != nil || len(sum) != sha256.Size || hex.EncodeToString(sum) != i.SHA256 {
		return ErrKnownUsageInventory
	}
	return nil
}

// BuildKnownUsageInventory sorts a copy so Node and backend can independently
// reproduce the same digest without depending on query/insertion order.
func BuildKnownUsageInventory(batches []KnownUsageBatch) (KnownUsageInventory, error) {
	items := append([]KnownUsageBatch(nil), batches...)
	sort.Slice(items, func(i, j int) bool { return items[i].BatchID < items[j].BatchID })
	h := sha256.New()
	_, _ = h.Write([]byte("vortex/committed-known-usage/v1\n"))
	for i, b := range items {
		if b.BatchID == "" || i > 0 && items[i-1].BatchID == b.BatchID {
			return KnownUsageInventory{}, ErrKnownUsageInventory
		}
		if (KnownUsageInventory{Version: 1, SHA256: b.SHA256}).Validate() != nil {
			return KnownUsageInventory{}, ErrKnownUsageInventory
		}
		raw, err := json.Marshal([]string{b.BatchID, b.SHA256})
		if err != nil {
			return KnownUsageInventory{}, err
		}
		_, _ = h.Write(raw)
		_, _ = h.Write([]byte{'\n'})
	}
	return KnownUsageInventory{Version: 1, Count: uint64(len(items)), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
