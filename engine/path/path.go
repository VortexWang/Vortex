package path

import (
	"errors"
	"sync"
)

var (
	ErrNoRoute         = errors.New("path: no authorized healthy route")
	ErrDuplicateHop    = errors.New("path: duplicate hop")
	ErrBypass          = errors.New("path: required hop bypass")
	ErrDraining        = errors.New("path: generation is draining")
	ErrStaleGeneration = errors.New("path: stale generation")
)

type NodeID [16]byte

type Candidate struct {
	Nodes      []NodeID
	Authorized bool
	Healthy    bool
}

func (c Candidate) valid() bool {
	if len(c.Nodes) == 0 || !c.Authorized || !c.Healthy {
		return false
	}
	seen := make(map[NodeID]struct{}, len(c.Nodes))
	for _, n := range c.Nodes {
		if n == (NodeID{}) {
			return false
		}
		if _, ok := seen[n]; ok {
			return false
		}
		seen[n] = struct{}{}
	}
	return true
}

type Snapshot struct {
	Generation           uint64
	Nodes                []NodeID
	Draining             bool
	ActiveTCP, ActiveUDP int
}

type Coordinator struct {
	mu         sync.Mutex
	now        func() uint64
	generation uint64
	active     []NodeID
	draining   map[uint64]*generation
	close      func(uint64, bool)
}
type generation struct {
	nodes    []NodeID
	draining bool
	tcp, udp int
}

func NewCoordinator(close func(uint64, bool)) *Coordinator {
	return &Coordinator{draining: make(map[uint64]*generation), close: close}
}

func Select(candidates []Candidate) (Candidate, error) {
	for _, c := range candidates {
		if c.valid() {
			return clone(c), nil
		}
	}
	return Candidate{}, ErrNoRoute
}

func (c *Coordinator) Activate(candidate Candidate) (uint64, error) {
	if !candidate.valid() {
		return 0, ErrNoRoute
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	if c.generation == 0 {
		c.generation = 1
	}
	if len(c.active) > 0 {
		c.markDrainingLocked(c.generation - 1)
	}
	c.active = append([]NodeID(nil), candidate.Nodes...)
	c.ensureGeneration(c.generation)
	return c.generation, nil
}

func (c *Coordinator) markDrainingLocked(gen uint64) {
	if g := c.draining[gen]; g != nil {
		g.draining = true
	}
}
func (c *Coordinator) ensureGeneration(gen uint64) *generation {
	if g := c.draining[gen]; g != nil {
		return g
	}
	if gen == c.generation {
		g := &generation{nodes: append([]NodeID(nil), c.active...)}
		c.draining[gen] = g
		return g
	}
	return nil
}

func (c *Coordinator) Open(gen uint64, udp bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.draining[gen]
	if g == nil || g.draining || gen != c.generation {
		return ErrDraining
	}
	if udp {
		g.udp++
	} else {
		g.tcp++
	}
	return nil
}
func (c *Coordinator) Close(gen uint64, udp bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.draining[gen]
	if g == nil {
		return
	}
	if udp {
		if g.udp > 0 {
			g.udp--
		}
	} else if g.tcp > 0 {
		g.tcp--
	}
	if g.draining && g.tcp == 0 && g.udp == 0 {
		delete(c.draining, gen)
		if c.close != nil {
			c.close(gen, false)
		}
	}
}
func (c *Coordinator) BeginDrain(gen uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.draining[gen]
	if g == nil {
		return ErrStaleGeneration
	}
	g.draining = true
	return nil
}
func (c *Coordinator) Snapshot(gen uint64) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.draining[gen]
	if !ok {
		return Snapshot{}, false
	}
	return Snapshot{Generation: gen, Nodes: append([]NodeID(nil), g.nodes...), Draining: g.draining, ActiveTCP: g.tcp, ActiveUDP: g.udp}, true
}
func clone(c Candidate) Candidate { c.Nodes = append([]NodeID(nil), c.Nodes...); return c }
