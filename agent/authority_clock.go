package agent

import (
	"fmt"
	"sync"
	"time"
)

// AuthorityClock is boot-local. Only a successfully authenticated control
// response may establish it. Persisted wall time never establishes trust after
// restart. Bounds include the elapsed boot interval and monotonic-clock drift.
// The random boot ID is the freshness challenge: an update for this boot
// cannot have been issued before the boot ID was created.
type AuthorityClock struct {
	mu                     sync.Mutex
	now                    func() time.Time
	received, lower, upper time.Time
	valid                  bool
	anchored               bool
}

func newAuthorityClock() *AuthorityClock { return &AuthorityClock{now: time.Now} }

const authorityClockMaxAge = 300 * time.Second
const authorityClockMaxBootstrap = 300 * time.Second

func (c *AuthorityClock) project(now time.Time) (time.Time, time.Time, bool) {
	elapsed := now.Sub(c.received)
	// Compare wall and monotonic progress; wall-clock steps invalidate trust.
	wallElapsed := now.Round(0).Sub(c.received.Round(0))
	difference := wallElapsed - elapsed
	if difference < 0 {
		difference = -difference
	}
	if !c.anchored || elapsed < 0 || difference > time.Second {
		return time.Time{}, time.Time{}, false
	}
	// A deliberately conservative 1000 ppm envelope plus 10 ms quantization.
	drift := elapsed/1000 + 10*time.Millisecond
	lower := c.lower.Add(elapsed - drift)
	// An authenticated lower bound remains a lower bound while local clock
	// continuity holds. Quantization uncertainty must not move it backwards
	// immediately after observing a newly issued authorization.
	if lower.Before(c.lower) {
		lower = c.lower
	}
	return lower, c.upper.Add(elapsed + drift), true
}

func (c *AuthorityClock) Bounds() (time.Time, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !c.valid || now.Sub(c.received) > authorityClockMaxAge {
		return time.Time{}, time.Time{}, false
	}
	return c.project(now)
}

func (c *AuthorityClock) observe(issued, bootStarted, received time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := received.Sub(bootStarted)
	if issued.IsZero() || bootStarted.IsZero() || elapsed < 0 || (!c.anchored && elapsed > authorityClockMaxBootstrap) {
		c.valid = false
		return fmt.Errorf("agent: authorization time response is not fresh")
	}
	lower, upper := issued, issued.Add(elapsed)
	previousLower, previousUpper, ok := c.project(received)
	if c.anchored && !ok {
		c.valid = false
		return fmt.Errorf("agent: local clock continuity lost")
	}
	if ok {
		if previousLower.After(lower) {
			lower = previousLower
		}
		if previousUpper.Before(upper) {
			upper = previousUpper
		}
		if upper.Before(lower) {
			c.valid = false
			return fmt.Errorf("agent: authenticated time intervals disagree")
		}
	}
	c.received, c.lower, c.upper, c.valid, c.anchored = received, lower, upper, true, true
	return nil
}

func (n *Node) TrustedTime() (time.Time, time.Time, bool) {
	if n == nil || n.clock == nil {
		return time.Time{}, time.Time{}, false
	}
	return n.clock.Bounds()
}
