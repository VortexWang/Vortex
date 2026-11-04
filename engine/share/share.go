package share

import (
	"errors"
	"sync"
)

var (
	ErrConflict = errors.New("share: fencing conflict")
	ErrPrepared = errors.New("share: prepared revision cannot be consumed")
	ErrCapacity = errors.New("share: connection share capacity exhausted")
	ErrDrain    = errors.New("share: previous generation is still draining")
	ErrHistory  = errors.New("share: revision history limit reached")
)

const maxRevisionHistory = 64

type Key struct{ Node, Boot [16]byte }
type Revision struct {
	Key                             Key
	Generation                      uint64
	Rate, Connections               int64
	InUse, OverlapLimit             int64
	Prepared, Active, RenewalClosed bool
	Draining                        bool
}
type Manager struct {
	mu        sync.Mutex
	revisions map[Key]map[uint64]*Revision
	prepared  map[Key]uint64
	active    map[Key]uint64
	fence     map[Key]uint64
	boot      map[[16]byte][16]byte
	requests  map[Key]map[[16]byte]prepareRecord
}

type prepareRecord struct {
	revision          uint64
	rate, connections int64
}

func NewManager() *Manager {
	return &Manager{revisions: make(map[Key]map[uint64]*Revision), prepared: make(map[Key]uint64), active: make(map[Key]uint64), fence: make(map[Key]uint64), boot: make(map[[16]byte][16]byte), requests: make(map[Key]map[[16]byte]prepareRecord)}
}

// BeginBoot fences every command and admission from the node's prior boot ID.
func (m *Manager) BeginBoot(key Key) error {
	if key.Node == ([16]byte{}) || key.Boot == ([16]byte{}) {
		return ErrConflict
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.boot[key.Node] = key.Boot
	if m.revisions[key] == nil {
		m.revisions[key] = make(map[uint64]*Revision)
	}
	return nil
}

func (m *Manager) currentBoot(key Key) bool {
	if key.Node == ([16]byte{}) || key.Boot == ([16]byte{}) {
		return false
	}
	boot, ok := m.boot[key.Node]
	if !ok {
		m.boot[key.Node] = key.Boot
		return true
	}
	return boot == key.Boot
}

func (m *Manager) Prepare(key Key, rate, connections int64) (Revision, error) {
	return m.prepare(key, [16]byte{}, rate, connections)
}

// PrepareWithID makes retries of one control transaction replay its original
// prepared revision while rejecting identity reuse with different values.
func (m *Manager) PrepareWithID(key Key, transaction [16]byte, rate, connections int64) (Revision, error) {
	if transaction == ([16]byte{}) {
		return Revision{}, ErrConflict
	}
	return m.prepare(key, transaction, rate, connections)
}

func (m *Manager) prepare(key Key, transaction [16]byte, rate, connections int64) (Revision, error) {
	if rate < 0 || connections < 0 {
		return Revision{}, ErrConflict
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentBoot(key) {
		return Revision{}, ErrConflict
	}
	if m.revisions[key] == nil {
		m.revisions[key] = make(map[uint64]*Revision)
	}
	if transaction != ([16]byte{}) {
		if m.requests[key] == nil {
			m.requests[key] = make(map[[16]byte]prepareRecord)
		}
		if old, ok := m.requests[key][transaction]; ok {
			if old.rate != rate || old.connections != connections {
				return Revision{}, ErrConflict
			}
			if r := m.revisions[key][old.revision]; r != nil {
				return *r, nil
			}
			return Revision{}, ErrConflict
		}
	}
	m.pruneLocked(key)
	if len(m.revisions[key]) >= maxRevisionHistory {
		return Revision{}, ErrHistory
	}
	if oldGen := m.prepared[key]; oldGen != 0 {
		if old := m.revisions[key][oldGen]; old != nil {
			old.Prepared = false
		}
	}
	m.fence[key]++
	r := &Revision{Key: key, Generation: m.fence[key], Rate: rate, Connections: connections, Prepared: true}
	m.revisions[key][r.Generation] = r
	m.prepared[key] = r.Generation
	if transaction != ([16]byte{}) {
		m.requests[key][transaction] = prepareRecord{revision: r.Generation, rate: rate, connections: connections}
	}
	return *r, nil
}

func (m *Manager) pruneLocked(key Key) {
	for generation, revision := range m.revisions[key] {
		if len(m.revisions[key]) < maxRevisionHistory {
			return
		}
		if revision.Active || revision.Prepared || revision.Draining || revision.InUse != 0 {
			continue
		}
		delete(m.revisions[key], generation)
		for transaction, record := range m.requests[key] {
			if record.revision == generation {
				delete(m.requests[key], transaction)
			}
		}
	}
}

func (m *Manager) Commit(key Key, generation uint64) (Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentBoot(key) {
		return Revision{}, ErrConflict
	}
	if generation == m.active[key] {
		if r := m.revisions[key][generation]; r != nil {
			return *r, nil
		}
	}
	if generation != m.prepared[key] {
		return Revision{}, ErrConflict
	}
	next := m.revisions[key][generation]
	if next == nil || !next.Prepared || generation != m.fence[key] {
		return Revision{}, ErrConflict
	}
	for gen, old := range m.revisions[key] {
		if old.Draining && old.InUse > 0 {
			return Revision{}, ErrDrain
		}
		if gen == m.active[key] {
			old.RenewalClosed, old.Draining, old.Active = true, old.InUse > 0, false
			next.OverlapLimit = max(old.Connections, next.Connections)
		}
	}
	if next.OverlapLimit == 0 {
		next.OverlapLimit = next.Connections
	}
	next.Prepared, next.Active = false, true
	m.active[key] = generation
	delete(m.prepared, key)
	return *next, nil
}

func (m *Manager) CloseRenewal(key Key, generation uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentBoot(key) {
		return ErrConflict
	}
	r := m.revisions[key][generation]
	if r == nil {
		return ErrConflict
	}
	r.RenewalClosed = true
	return nil
}

type Lease struct {
	manager    *Manager
	key        Key
	generation uint64
	once       sync.Once
}

func (m *Manager) Acquire(key Key) (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentBoot(key) {
		return nil, ErrConflict
	}
	gen := m.active[key]
	active := m.revisions[key][gen]
	if active == nil || !active.Active || active.RenewalClosed {
		return nil, ErrPrepared
	}
	var total int64
	for _, r := range m.revisions[key] {
		total += r.InUse
	}
	limit := active.OverlapLimit
	if limit == 0 {
		limit = active.Connections
	}
	if active.InUse >= active.Connections || total >= limit {
		return nil, ErrCapacity
	}
	active.InUse++
	return &Lease{manager: m, key: key, generation: gen}, nil
}

func (l *Lease) Generation() uint64 { return l.generation }
func (l *Lease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		m := l.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		r := m.revisions[l.key][l.generation]
		if r == nil || r.InUse == 0 {
			return
		}
		r.InUse--
		if r.Draining && r.InUse == 0 {
			r.Draining = false
		}
	})
}

func (m *Manager) Get(key Key) (Revision, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gen := m.active[key]
	if gen == 0 {
		gen = m.prepared[key]
	}
	r := m.revisions[key][gen]
	if r == nil {
		return Revision{}, false
	}
	return *r, true
}

func (m *Manager) Revisions(key Key) []Revision {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Revision, 0, len(m.revisions[key]))
	for _, r := range m.revisions[key] {
		result = append(result, *r)
	}
	return result
}
