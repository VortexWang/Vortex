package agent

import "fmt"

// Bound per-connection identity memory without forgetting an earlier message.
// At the bound, reconnect and recover from durable request/outbox identities.
// The triggering message must not be applied on this connection.
const maxControlMessageIDs = 4096

type controlReplayGuard struct {
	last uint64
	ids  map[string]struct{}
}

func (g *controlReplayGuard) accept(sequence uint64, id string) error {
	if sequence == 0 || sequence <= g.last || !canonicalUUID(id) {
		return fmt.Errorf("agent: invalid control message identity or sequence")
	}
	if _, exists := g.ids[id]; exists {
		return fmt.Errorf("agent: duplicate control message id")
	}
	if len(g.ids) >= maxControlMessageIDs {
		return fmt.Errorf("agent: control message identity limit reached; reconnect required")
	}
	if g.ids == nil {
		g.ids = make(map[string]struct{})
	}
	g.ids[id] = struct{}{}
	g.last = sequence
	return nil
}
