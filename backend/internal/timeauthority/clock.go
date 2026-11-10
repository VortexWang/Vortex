// Package timeauthority exposes conservative UTC intervals from the synchronized
// Linux kernel clock. Missing synchronization denies new rights. Clock state is
// process local; restart never restores trust from a persisted wall timestamp.
package timeauthority

import (
	"sync"
	"time"
)

type Clock struct {
	mu       sync.Mutex
	read     func() (time.Time, time.Duration, bool)
	now      func() time.Time
	previous time.Time
	invalid  bool
}

func New() *Clock { return &Clock{read: kernelBounds, now: time.Now} }
func (c *Clock) Bounds() (time.Time, time.Time, bool) {
	if c == nil {
		return time.Time{}, time.Time{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !c.previous.IsZero() {
		elapsed := now.Sub(c.previous)
		wall := now.Round(0).Sub(c.previous.Round(0))
		difference := wall - elapsed
		if difference < 0 {
			difference = -difference
		}
		if elapsed < 0 || difference > time.Second {
			c.invalid = true
		}
	}
	c.previous = now
	if c.invalid {
		return time.Time{}, time.Time{}, false
	}
	at, errorBound, ok := c.read()
	if !ok || at.IsZero() || errorBound < 0 || errorBound > time.Second-10*time.Millisecond {
		return time.Time{}, time.Time{}, false
	}
	errorBound += 10 * time.Millisecond
	return at.Add(-errorBound).UTC(), at.Add(errorBound).UTC(), true
}
