package vlink

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type TransportMode string

const (
	ModeMux  TransportMode = "mux"
	ModePool TransportMode = "pool"
	ModeAuto TransportMode = "auto"
)

func SelectTransportMode(configured TransportMode, network StreamNetwork, median time.Duration, hasHistory bool) TransportMode {
	if network == StreamUDP {
		return ModeMux
	}
	if configured == ModeAuto {
		if hasHistory && median < 10*time.Second {
			return ModePool
		}
		return ModeMux
	}
	if configured != ModePool {
		return ModeMux
	}
	return ModePool
}

type latencySample struct {
	at  time.Time
	lat time.Duration
}

type FirstPacketHistory struct {
	mu      sync.Mutex
	window  time.Duration
	max     int
	now     func() time.Time
	samples []latencySample
}

func NewFirstPacketHistory(maxSamples int, now func() time.Time) (*FirstPacketHistory, error) {
	if maxSamples < 1 {
		return nil, errors.New("vlink: first-packet history needs a positive sample bound")
	}
	if now == nil {
		now = time.Now
	}
	return &FirstPacketHistory{window: 10 * time.Minute, max: maxSamples, now: now}, nil
}

func (h *FirstPacketHistory) Observe(duration time.Duration) error {
	if duration < 0 {
		return errors.New("vlink: first-packet duration cannot be negative")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked()
	h.samples = append(h.samples, latencySample{at: h.now(), lat: duration})
	if len(h.samples) > h.max {
		copy(h.samples, h.samples[len(h.samples)-h.max:])
		h.samples = h.samples[:h.max]
	}
	return nil
}

func (h *FirstPacketHistory) Median() (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked()
	if len(h.samples) == 0 {
		return 0, false
	}
	values := make([]time.Duration, len(h.samples))
	for i, sample := range h.samples {
		values[i] = sample.lat
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle], true
	}
	return values[middle-1] + (values[middle]-values[middle-1])/2, true
}

func (h *FirstPacketHistory) Mode(configured TransportMode, network StreamNetwork) TransportMode {
	median, hasHistory := h.Median()
	return SelectTransportMode(configured, network, median, hasHistory)
}

func (h *FirstPacketHistory) pruneLocked() {
	cutoff := h.now().Add(-h.window)
	first := 0
	for first < len(h.samples) && !h.samples[first].at.After(cutoff) {
		first++
	}
	if first > 0 {
		copy(h.samples, h.samples[first:])
		h.samples = h.samples[:len(h.samples)-first]
	}
}

type PoolConfig struct {
	MinLinks     int
	MaxLinks     int
	HardCap      int
	ScaleStep    int
	IdleFraction float64
	Now          func() time.Time
}

type PooledLink interface {
	Healthy() bool
	Close() error
}

type LinkDialer func(context.Context) (PooledLink, error)

type poolEntry struct {
	link     PooledLink
	active   int
	lastUsed time.Time
}

type LinkPool struct {
	mu       sync.Mutex
	config   PoolConfig
	dial     LinkDialer
	entries  []*poolEntry
	dialing  int
	changed  chan struct{}
	closed   bool
	closeErr error
}

type PoolSnapshot struct {
	Total     int
	Active    int
	Idle      int
	Unhealthy int
	Dialing   int
	Closed    bool
}

func NewLinkPool(config PoolConfig, dial LinkDialer) (*LinkPool, error) {
	if dial == nil {
		return nil, errors.New("vlink: link pool requires a dialer")
	}
	if config.HardCap == 0 {
		config.HardCap = 4
	}
	if config.MaxLinks == 0 {
		config.MaxLinks = config.HardCap
	}
	if config.MinLinks == 0 {
		config.MinLinks = 1
	}
	if config.ScaleStep == 0 {
		config.ScaleStep = 1
	}
	if config.IdleFraction == 0 {
		config.IdleFraction = 0.5
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MinLinks < 0 || config.MaxLinks < config.MinLinks || config.HardCap < config.MaxLinks || config.ScaleStep < 1 || config.IdleFraction < 0 || config.IdleFraction >= 1 {
		return nil, errors.New("vlink: invalid link pool bounds")
	}
	return &LinkPool{config: config, dial: dial, changed: make(chan struct{})}, nil
}

// Acquire leases one link in pool mode and shares the least-loaded healthy
// link in mux mode. New dials are reserved under the hard cap before dialing.
func (p *LinkPool) Acquire(ctx context.Context, mode TransportMode) (PooledLink, func(), error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, nil, ErrWindowClosed
		}
		p.removeUnhealthyLocked()
		selected := p.selectLocked(mode)
		if selected != nil {
			selected.active++
			selected.lastUsed = p.config.Now()
			p.mu.Unlock()
			var once sync.Once
			return selected.link, func() {
				once.Do(func() {
					p.mu.Lock()
					if selected.active > 0 {
						selected.active--
					}
					selected.lastUsed = p.config.Now()
					p.broadcastLocked()
					p.mu.Unlock()
				})
			}, nil
		}
		if len(p.entries)+p.dialing < p.config.MaxLinks && len(p.entries)+p.dialing < p.config.HardCap {
			p.dialing++
			p.mu.Unlock()
			link, err := p.dial(ctx)
			p.mu.Lock()
			p.dialing--
			if err == nil && link != nil && link.Healthy() && !p.closed {
				entry := &poolEntry{link: link, active: 1, lastUsed: p.config.Now()}
				p.entries = append(p.entries, entry)
				p.broadcastLocked()
				p.mu.Unlock()
				var once sync.Once
				return link, func() { once.Do(func() { p.release(entry) }) }, nil
			}
			p.broadcastLocked()
			closed := p.closed
			p.mu.Unlock()
			if link != nil {
				_ = link.Close()
			}
			if err != nil {
				return nil, nil, err
			}
			if closed {
				return nil, nil, ErrWindowClosed
			}
			continue
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-changed:
		}
	}
}

func (p *LinkPool) Reconcile(demand int, healthy bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.removeUnhealthyLocked()
	target := p.desired(demand, healthy)
	if len(p.entries) > target {
		idle := make([]int, 0, len(p.entries))
		for i, entry := range p.entries {
			if entry.active == 0 {
				idle = append(idle, i)
			}
		}
		sort.Slice(idle, func(i, j int) bool { return p.entries[idle[i]].lastUsed.Before(p.entries[idle[j]].lastUsed) })
		removeCount := min(len(p.entries)-target, len(idle))
		// Choose the oldest idle entries, then delete in descending storage
		// order so removals cannot shift the remaining selected indices.
		idle = idle[:removeCount]
		sort.Sort(sort.Reverse(sort.IntSlice(idle)))
		for _, index := range idle {
			entry := p.entries[index]
			p.entries = append(p.entries[:index], p.entries[index+1:]...)
			_ = entry.link.Close()
		}
	}
	p.broadcastLocked()
}

// Scale prewarms toward the configured target. Acquire still grows on demand
// and reserves every concurrent dial against the hard cap.
func (p *LinkPool) Scale(ctx context.Context, demand int, healthy bool) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrWindowClosed
	}
	p.removeUnhealthyLocked()
	target := p.desired(demand, healthy)
	p.mu.Unlock()
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return ErrWindowClosed
		}
		if len(p.entries) >= target || len(p.entries)+p.dialing >= p.config.HardCap {
			p.mu.Unlock()
			break
		}
		p.dialing++
		p.mu.Unlock()
		link, err := p.dial(ctx)
		p.mu.Lock()
		p.dialing--
		if err == nil && link != nil && link.Healthy() && !p.closed && len(p.entries) < p.config.MaxLinks && len(p.entries) < p.config.HardCap {
			p.entries = append(p.entries, &poolEntry{link: link, lastUsed: p.config.Now()})
			p.broadcastLocked()
			p.mu.Unlock()
			continue
		}
		p.broadcastLocked()
		p.mu.Unlock()
		if link != nil {
			_ = link.Close()
		}
		if err != nil {
			return err
		}
		break
	}
	p.Reconcile(demand, healthy)
	return nil
}

func (p *LinkPool) Snapshot() PoolSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := PoolSnapshot{Total: len(p.entries), Dialing: p.dialing, Closed: p.closed}
	for _, entry := range p.entries {
		snapshot.Active += entry.active
		if entry.active == 0 {
			snapshot.Idle++
		}
		if !entry.link.Healthy() {
			snapshot.Unhealthy++
		}
	}
	return snapshot
}

func (p *LinkPool) Close() error {
	p.mu.Lock()
	if p.closed {
		err := p.closeErr
		p.mu.Unlock()
		return err
	}
	p.closed = true
	entries := p.entries
	p.entries = nil
	p.broadcastLocked()
	p.mu.Unlock()
	var closeErr error
	for _, entry := range entries {
		closeErr = errors.Join(closeErr, entry.link.Close())
	}
	p.mu.Lock()
	p.closeErr = closeErr
	p.mu.Unlock()
	return closeErr
}

func (p *LinkPool) selectLocked(mode TransportMode) *poolEntry {
	var selected *poolEntry
	for _, entry := range p.entries {
		if !entry.link.Healthy() || mode == ModePool && entry.active != 0 {
			continue
		}
		if selected == nil || entry.active < selected.active || entry.active == selected.active && entry.lastUsed.Before(selected.lastUsed) {
			selected = entry
		}
	}
	return selected
}

func (p *LinkPool) desired(demand int, healthy bool) int {
	if !healthy || demand <= 0 {
		return p.config.MinLinks
	}
	capacity := float64(p.config.ScaleStep) * (1 - p.config.IdleFraction)
	needed := p.config.MinLinks + int(float64(demand)/capacity)
	if float64(needed-p.config.MinLinks)*capacity < float64(demand) {
		needed++
	}
	if needed > p.config.MaxLinks {
		needed = p.config.MaxLinks
	}
	return needed
}

func (p *LinkPool) removeUnhealthyLocked() {
	for i := 0; i < len(p.entries); {
		if p.entries[i].link.Healthy() {
			i++
			continue
		}
		entry := p.entries[i]
		p.entries = append(p.entries[:i], p.entries[i+1:]...)
		_ = entry.link.Close()
	}
}

func (p *LinkPool) release(entry *poolEntry) {
	p.mu.Lock()
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = p.config.Now()
	p.broadcastLocked()
	p.mu.Unlock()
}

func (p *LinkPool) broadcastLocked() { close(p.changed); p.changed = make(chan struct{}) }

func (c PoolConfig) String() string {
	return fmt.Sprintf("min=%d max=%d hard_cap=%d", c.MinLinks, c.MaxLinks, c.HardCap)
}

type DuplicateLinkRegistry struct {
	mu    sync.Mutex
	local NodeID
	links map[NodeID]registeredLink
}

type registeredLink struct {
	initiator NodeID
	rank      [32]byte
	link      interface{ Close(error) }
}

func NewDuplicateLinkRegistry(local NodeID) *DuplicateLinkRegistry {
	return &DuplicateLinkRegistry{local: local, links: make(map[NodeID]registeredLink)}
}

// Register prefers the link initiated by the lexicographically lower node ID,
// then uses the transcript rank to choose consistently between same-direction
// duplicates. The rank should be derived from the authenticated HELLO nonces.
func (r *DuplicateLinkRegistry) Register(peer, initiator NodeID, rank [32]byte, link interface{ Close(error) }) bool {
	if (initiator != r.local && initiator != peer) || r.local == peer {
		link.Close(ErrProtocol)
		return false
	}
	r.mu.Lock()
	current, exists := r.links[peer]
	if !exists {
		r.links[peer] = registeredLink{initiator: initiator, rank: rank, link: link}
		r.mu.Unlock()
		return true
	}
	wantedInitiator := r.local
	if peer.String() < r.local.String() {
		wantedInitiator = peer
	}
	newPreferred := initiator == wantedInitiator
	currentPreferred := current.initiator == wantedInitiator
	newWins := newPreferred && !currentPreferred || newPreferred == currentPreferred && string(rank[:]) < string(current.rank[:])
	if newWins {
		r.links[peer] = registeredLink{initiator: initiator, rank: rank, link: link}
		r.mu.Unlock()
		current.link.Close(ErrProtocol)
		return true
	}
	r.mu.Unlock()
	link.Close(ErrProtocol)
	return false
}

func (r *DuplicateLinkRegistry) Remove(peer NodeID, link interface{ Close(error) }) {
	r.mu.Lock()
	if current, ok := r.links[peer]; ok && current.link == link {
		delete(r.links, peer)
	}
	r.mu.Unlock()
}

type Backoff struct {
	Initial time.Duration
	Maximum time.Duration
	Jitter  func(time.Duration) time.Duration
	Attempt int
}

func (b *Backoff) Next() time.Duration {
	initial := b.Initial
	if initial <= 0 {
		initial = 250 * time.Millisecond
	}
	maximum := b.Maximum
	if maximum <= 0 {
		maximum = 30 * time.Second
	}
	delay := initial
	for i := 0; i < b.Attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	b.Attempt++
	if b.Jitter != nil {
		delay = b.Jitter(delay)
		if delay < 0 {
			delay = 0
		}
		if delay > maximum {
			delay = maximum
		}
	}
	if delay < 0 {
		delay = 0
	}
	return delay
}
