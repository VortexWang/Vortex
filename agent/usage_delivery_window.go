package agent

import "sync"

type usageDeliveryIdentity struct {
	id      string
	unknown bool
}

// A transport window bounds unacknowledged report identities on one connection.
// It grants no spending authority and never changes a durable outbox/debt row.
// Retry/reconnect may forget these hints; only a committed ACK releases a slot
// during a scan, and periodic replay retransmits retained durable rows.
type usageDeliveryWindow struct {
	mu      sync.Mutex
	limit   int
	pending map[usageDeliveryIdentity]struct{}
}

func (w *usageDeliveryWindow) remaining() int {
	if w == nil {
		return 32
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.limit - len(w.pending)
}
func (w *usageDeliveryWindow) claim(unknown bool, id string) bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	key := usageDeliveryIdentity{id, unknown}
	if _, ok := w.pending[key]; ok {
		return true
	}
	if len(w.pending) >= w.limit {
		return false
	}
	if w.pending == nil {
		w.pending = make(map[usageDeliveryIdentity]struct{})
	}
	w.pending[key] = struct{}{}
	return true
}
func (w *usageDeliveryWindow) confirm(unknown bool, ids ...string) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	removed := false
	for _, id := range ids {
		key := usageDeliveryIdentity{id, unknown}
		if _, ok := w.pending[key]; ok {
			delete(w.pending, key)
			removed = true
		}
	}
	return removed
}
func (w *usageDeliveryWindow) reset() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.pending)
}

// Refill half a window at a time, so individual ACKs do not turn sequence
// reservations into one FULL commit per report. Sparse tails still send on
// their initial/new-row turn and lost ACKs recover on periodic replay.
func (w *usageDeliveryWindow) ready() bool {
	if w == nil {
		return true
	}
	return w.remaining() >= (w.limit+1)/2
}
