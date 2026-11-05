package apply

import (
	"errors"
	"sync"
)

var (
	ErrConflict = errors.New("apply: invalid transaction transition")
	ErrUnknown  = errors.New("apply: activation unknown")
)

type State string

const (
	Prepared          State = "prepared"
	Committing        State = "committing"
	Active            State = "active"
	Aborted           State = "aborted"
	ActivationUnknown State = "activation-unknown"
	Released          State = "released"
)

type Transaction struct {
	ID         [16]byte
	Generation uint64
	State      State
}
type Manager struct {
	mu     sync.Mutex
	tx     map[[16]byte]Transaction
	active uint64
}

func NewManager() *Manager { return &Manager{tx: make(map[[16]byte]Transaction)} }
func (m *Manager) Prepare(id [16]byte, gen uint64) (Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == ([16]byte{}) || gen == 0 {
		return Transaction{}, ErrConflict
	}
	if old, ok := m.tx[id]; ok {
		if old.Generation != gen {
			return Transaction{}, ErrConflict
		}
		return old, nil
	}
	if gen <= m.active {
		return Transaction{}, ErrConflict
	}
	t := Transaction{ID: id, Generation: gen, State: Prepared}
	m.tx[id] = t
	return t, nil
}
func (m *Manager) Commit(id [16]byte, uncertain bool) (Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tx[id]
	if !ok {
		return Transaction{}, ErrConflict
	}
	if t.State == Active {
		return t, nil
	}
	if t.State == ActivationUnknown {
		return t, ErrUnknown
	}
	if t.State != Prepared {
		return Transaction{}, ErrConflict
	}
	if uncertain {
		t.State = ActivationUnknown
		m.tx[id] = t
		return t, ErrUnknown
	}
	if t.Generation <= m.active {
		return Transaction{}, ErrConflict
	}
	t.State = Active
	m.active = t.Generation
	m.tx[id] = t
	return t, nil
}
func (m *Manager) Abort(id [16]byte) (Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tx[id]
	if !ok {
		return Transaction{}, ErrConflict
	}
	if t.State == Aborted || t.State == Released {
		return t, nil
	}
	if t.State == Active || t.State == ActivationUnknown || t.State == Committing {
		return Transaction{}, ErrConflict
	}
	t.State = Aborted
	m.tx[id] = t
	return t, nil
}
func (m *Manager) Release(id [16]byte, provenInactive bool) (Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tx[id]
	if !ok || !provenInactive || t.State == Active {
		return Transaction{}, ErrConflict
	}
	t.State = Released
	m.tx[id] = t
	return t, nil
}
