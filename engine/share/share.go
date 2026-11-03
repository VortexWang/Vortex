package share

import (
	"errors"
	"sync"
)

var (
	ErrConflict = errors.New("share: fencing conflict")
	ErrPrepared = errors.New("share: prepared revision cannot be consumed")
)

type Key struct{ Node, Boot [16]byte }
type Revision struct {
	Key                             Key
	Generation                      uint64
	Rate, Connections               int64
	Prepared, Active, RenewalClosed bool
}
type Manager struct {
	mu      sync.Mutex
	current map[Key]Revision
	fence   map[Key]uint64
}

func NewManager() *Manager {
	return &Manager{current: make(map[Key]Revision), fence: make(map[Key]uint64)}
}
func (m *Manager) Prepare(key Key, rate, connections int64) (Revision, error) {
	if rate < 0 || connections < 0 {
		return Revision{}, ErrConflict
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fence[key]++
	r := Revision{Key: key, Generation: m.fence[key], Rate: rate, Connections: connections, Prepared: true}
	m.current[key] = r
	return r, nil
}
func (m *Manager) Commit(key Key, generation uint64) (Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.current[key]
	if !ok || r.Generation != generation || !r.Prepared {
		return Revision{}, ErrConflict
	}
	r.Prepared = false
	r.Active = true
	m.current[key] = r
	return r, nil
}
func (m *Manager) CloseRenewal(key Key, generation uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.current[key]
	if !ok || r.Generation != generation {
		return ErrConflict
	}
	r.RenewalClosed = true
	m.current[key] = r
	return nil
}
func (m *Manager) Get(key Key) (Revision, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.current[key]
	return r, ok
}
