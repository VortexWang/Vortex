package accounting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	engineaccounting "vortex/engine/accounting"
)

type Event struct {
	BatchID   uint64                      `json:"batch_id"`
	Flow      engineaccounting.Flow       `json:"flow"`
	Direction engineaccounting.Direction  `json:"direction"`
	Outcome   engineaccounting.Resolution `json:"outcome"`
	Reserved  int64                       `json:"reserved"`
	Used      int64                       `json:"used"`
}

type Gate struct {
	mu       sync.Mutex
	sequence atomic.Uint64
	chunks   [][]Event
	count    int
	max      int
}

// Fixed-sized chunks avoid copying millions of live events during append.
// A large slice growth can otherwise stall forwarding and WINDOW updates.
const eventChunkSize = 4096

func NewGate(maxEvents int) (*Gate, error) {
	if maxEvents < 1 {
		return nil, errors.New("memory accounting: max events must be positive")
	}
	return &Gate{max: maxEvents}, nil
}

func (g *Gate) Open(_ context.Context, flow engineaccounting.Flow) (engineaccounting.Session, error) {
	if flow.Key == "" {
		return nil, engineaccounting.ErrDenied
	}
	return &session{gate: g, flow: flow}, nil
}

func (g *Gate) Events() []Event {
	g.mu.Lock()
	defer g.mu.Unlock()
	events := make([]Event, 0, g.count)
	for _, chunk := range g.chunks {
		events = append(events, chunk...)
	}
	return events
}

func (g *Gate) WriteJSONL(w io.Writer) error {
	g.mu.Lock()
	// Copy only slice headers. Appended event values are immutable; capturing
	// each length gives a consistent snapshot without a full event copy.
	chunks := append([][]Event(nil), g.chunks...)
	g.mu.Unlock()
	encoder := json.NewEncoder(w)
	for _, chunk := range chunks {
		for _, event := range chunk {
			if err := encoder.Encode(event); err != nil {
				return fmt.Errorf("memory accounting: write event: %w", err)
			}
		}
	}
	return nil
}

type session struct {
	gate   *Gate
	flow   engineaccounting.Flow
	closed atomic.Bool
}

func (s *session) Reserve(_ context.Context, direction engineaccounting.Direction, amount int64) (*engineaccounting.Batch, error) {
	if s.closed.Load() || amount < 0 {
		return nil, engineaccounting.ErrDenied
	}
	id := s.gate.sequence.Add(1)
	return engineaccounting.NewBatch(amount, func(_ context.Context, outcome engineaccounting.Outcome) error {
		s.gate.mu.Lock()
		defer s.gate.mu.Unlock()
		if s.gate.count >= s.gate.max {
			return errors.New("memory accounting: event capacity exhausted")
		}
		last := len(s.gate.chunks) - 1
		if last < 0 || len(s.gate.chunks[last]) == cap(s.gate.chunks[last]) {
			size := min(eventChunkSize, s.gate.max-s.gate.count)
			s.gate.chunks = append(s.gate.chunks, make([]Event, 0, size))
			last++
		}
		s.gate.chunks[last] = append(s.gate.chunks[last], Event{BatchID: id, Flow: s.flow, Direction: direction, Outcome: outcome.Resolution, Reserved: outcome.Reserved, Used: outcome.Used})
		s.gate.count++
		return nil
	})
}

func (s *session) Close() error {
	s.closed.Store(true)
	return nil
}
