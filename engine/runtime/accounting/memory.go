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
	events   []Event
	max      int
}

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
	return append([]Event(nil), g.events...)
}

func (g *Gate) WriteJSONL(w io.Writer) error {
	encoder := json.NewEncoder(w)
	for _, event := range g.Events() {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("memory accounting: write event: %w", err)
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
		if len(s.gate.events) >= s.gate.max {
			return errors.New("memory accounting: event capacity exhausted")
		}
		s.gate.events = append(s.gate.events, Event{BatchID: id, Flow: s.flow, Direction: direction, Outcome: outcome.Resolution, Reserved: outcome.Reserved, Used: outcome.Used})
		return nil
	})
}

func (s *session) Close() error {
	s.closed.Store(true)
	return nil
}
