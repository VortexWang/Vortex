package agent

import (
	"testing"
	"time"
)

func TestAuthorityClockNeverRetreatsBelowAuthenticatedIssue(t *testing.T) {
	local := time.Now()
	now := local
	c := newAuthorityClock()
	c.now = func() time.Time { return now }
	issued := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if err := c.observe(issued, local.Add(-time.Second), local); err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{0, time.Nanosecond, time.Millisecond, 10 * time.Millisecond, time.Second} {
		now = local.Add(elapsed)
		low, high, trusted := c.Bounds()
		if !trusted || low.Before(issued) || low.After(issued.Add(elapsed)) || high.Before(issued.Add(elapsed)) {
			t.Fatalf("elapsed=%v bounds=%v..%v trusted=%v", elapsed, low, high, trusted)
		}
	}
}

func TestAuthorityClockRequiresFreshResponseAndExpires(t *testing.T) {
	local := time.Now()
	now := local
	c := newAuthorityClock()
	c.now = func() time.Time { return now }
	if _, _, ok := c.Bounds(); ok {
		t.Fatal("boot acquired trust from local time")
	}
	issued := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if err := c.observe(issued, local.Add(-time.Second), local); err != nil {
		t.Fatal(err)
	}
	low, high, ok := c.Bounds()
	if !ok || low.After(issued) || high.Before(issued.Add(time.Second)) {
		t.Fatalf("bounds %v %v %v", low, high, ok)
	}
	now = local.Add(20 * time.Second)
	low, high, ok = c.Bounds()
	if !ok || low.After(issued.Add(20*time.Second)) || high.Before(issued.Add(21*time.Second)) {
		t.Fatalf("advance %v %v %v", low, high, ok)
	}
	now = local.Add(authorityClockMaxAge + time.Nanosecond)
	if _, _, ok := c.Bounds(); ok {
		t.Fatal("expired clock stayed trusted")
	}
	fresh := newAuthorityClock()
	if _, _, ok := fresh.Bounds(); ok {
		t.Fatal("new boot inherited trust")
	}
}

func TestAuthorityClockRejectsDelayedOrContradictoryEvidence(t *testing.T) {
	now := time.Now()
	c := newAuthorityClock()
	c.now = func() time.Time { return now }
	if err := c.observe(now, now.Add(-301*time.Second), now); err == nil {
		t.Fatal("delayed response trusted")
	}
	if err := c.observe(now, now.Add(-time.Second), now); err != nil {
		t.Fatal(err)
	}
	if err := c.observe(now.Add(-time.Hour), now.Add(-time.Second), now); err == nil {
		t.Fatal("rollback of authenticated clock trusted")
	}
	if _, _, ok := c.Bounds(); ok {
		t.Fatal("contradiction retained trust")
	}
}

func TestAuthorityClockReplayDoesNotTurnTimeBackAfterExpiry(t *testing.T) {
	now := time.Now()
	boot := now
	c := newAuthorityClock()
	c.now = func() time.Time { return now }
	issued := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if err := c.observe(issued, boot, now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(301 * time.Second)
	if _, _, trusted := c.Bounds(); trusted {
		t.Fatal("stale clock is trusted")
	}
	if err := c.observe(issued, boot, now); err != nil {
		t.Fatal(err)
	}
	low, _, trusted := c.Bounds()
	if !trusted || low.Before(issued.Add(300*time.Second)) {
		t.Fatalf("replay moved time back: %v %v", low, trusted)
	}
}
