package timeauthority

import (
	"testing"
	"time"
)

func TestTrustedClockBoundsAndMissingSynchronization(t *testing.T) {
	at := time.Now()
	trusted := false
	bound := 100 * time.Millisecond
	c := &Clock{now: func() time.Time { return at }, read: func() (time.Time, time.Duration, bool) { return at, bound, trusted }}
	if _, _, ok := c.Bounds(); ok {
		t.Fatal("unsynchronized wall time trusted")
	}
	trusted = true
	lo, hi, ok := c.Bounds()
	if !ok || !lo.Equal(at.Add(-110*time.Millisecond)) || !hi.Equal(at.Add(110*time.Millisecond)) {
		t.Fatal(lo, hi, ok)
	}
	bound = 995 * time.Millisecond
	if _, _, ok = c.Bounds(); ok {
		t.Fatal("safety margin exceeded one-second bound")
	}
	bound = 2 * time.Second
	if _, _, ok = c.Bounds(); ok {
		t.Fatal("excess uncertainty trusted")
	}
	bound = -time.Second
	if _, _, ok = c.Bounds(); ok {
		t.Fatal("negative uncertainty trusted")
	}
	bound = 100 * time.Millisecond
	// Before a known prior interval is backward even without monotonic metadata.
	c.previous = at.Add(time.Second)
	if _, _, ok = c.Bounds(); ok {
		t.Fatal("backward jump trusted")
	}
	c.previous = time.Time{}
	if _, _, ok = c.Bounds(); ok {
		t.Fatal("clock discontinuity reopened without process restart")
	}
}
