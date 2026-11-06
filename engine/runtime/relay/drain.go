package relay

import "context"

type Stats struct {
	ActiveSessions int    `json:"active_sessions"`
	DrainState     string `json:"drain_state"`
}

// BeginDrain linearizes with stream acceptance. Streams already accepted may
// finish; subsequent OPENs, even on existing muxes, cannot reach authorization.
func (s *Service) BeginDrain() {
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()
}
func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := "running"
	if s.draining {
		state = "draining"
		if s.active == 0 {
			state = "drained"
		}
	}
	return Stats{ActiveSessions: s.active, DrainState: state}
}
func (s *Service) Drain(ctx context.Context) error {
	s.mu.Lock()
	s.draining = true
	if s.active == 0 {
		s.mu.Unlock()
		return nil
	}
	idle := s.idle
	s.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
