package vlink

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectTransportModeBoundaries(t *testing.T) {
	if got := SelectTransportMode(ModeAuto, StreamUDP, time.Millisecond, true); got != ModeMux {
		t.Fatalf("UDP mode=%s", got)
	}
	if got := SelectTransportMode(ModeAuto, StreamTCP, 9999*time.Millisecond, true); got != ModePool {
		t.Fatalf("short TCP mode=%s", got)
	}
	for _, sample := range []struct {
		median  time.Duration
		history bool
	}{{10 * time.Second, true}, {0, false}} {
		if got := SelectTransportMode(ModeAuto, StreamTCP, sample.median, sample.history); got != ModeMux {
			t.Fatalf("TCP mode at median=%s history=%v: %s", sample.median, sample.history, got)
		}
	}
}

func TestFirstPacketHistoryUsesBoundedTenMinuteMedian(t *testing.T) {
	now := time.Unix(100, 0)
	history, err := NewFirstPacketHistory(3, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 12 * time.Second} {
		if err := history.Observe(sample); err != nil {
			t.Fatal(err)
		}
	}
	median, ok := history.Median()
	if !ok || median != 8*time.Second {
		t.Fatalf("median=%s present=%v", median, ok)
	}
	if got := history.Mode(ModeAuto, StreamTCP); got != ModePool {
		t.Fatalf("auto mode=%s", got)
	}
	now = now.Add(10 * time.Minute)
	if _, ok := history.Median(); ok {
		t.Fatal("retained expired samples")
	}
	if got := history.Mode(ModeAuto, StreamTCP); got != ModeMux {
		t.Fatalf("no-history mode=%s", got)
	}
}

func TestLinkPoolHardCapAcrossConcurrentPoolModeAcquires(t *testing.T) {
	var dials atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	pool, err := NewLinkPool(PoolConfig{MinLinks: 1, MaxLinks: 2, HardCap: 2}, func(context.Context) (PooledLink, error) {
		dials.Add(1)
		time.Sleep(5 * time.Millisecond)
		return &fakePooledLink{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			link, release, err := pool.Acquire(context.Background(), ModePool)
			if err != nil {
				t.Error(err)
				return
			}
			if link == nil {
				t.Error("nil link")
				return
			}
			count := active.Add(1)
			for previous := maxActive.Load(); count > previous && !maxActive.CompareAndSwap(previous, count); previous = maxActive.Load() {
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	wg.Wait()
	if dials.Load() > 2 || maxActive.Load() > 2 || pool.Snapshot().Total > 2 {
		t.Fatalf("dials=%d max leases=%d snapshot=%+v", dials.Load(), maxActive.Load(), pool.Snapshot())
	}
}

func TestLinkPoolScalesDownAndRemovesUnhealthyLinks(t *testing.T) {
	var next atomic.Int32
	pool, err := NewLinkPool(PoolConfig{MinLinks: 1, MaxLinks: 4, HardCap: 4, ScaleStep: 1, IdleFraction: 0.5}, func(context.Context) (PooledLink, error) {
		return &fakePooledLink{id: next.Add(1)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Scale(context.Background(), 3, true); err != nil {
		t.Fatal(err)
	}
	if got := pool.Snapshot().Total; got != 4 {
		t.Fatalf("scaled total=%d, want 4", got)
	}
	pool.Reconcile(0, true)
	if got := pool.Snapshot().Total; got != 1 {
		t.Fatalf("scaled down total=%d, want 1", got)
	}
	link := pool.entries[0].link.(*fakePooledLink)
	link.unhealthy.Store(true)
	if _, _, err := pool.Acquire(context.Background(), ModeMux); err != nil {
		t.Fatal(err)
	}
	if snapshot := pool.Snapshot(); snapshot.Unhealthy != 0 || snapshot.Total != 1 {
		t.Fatalf("unhealthy link retained: %+v", snapshot)
	}
	_ = pool.Close()
}

func TestBackoffIsCappedAndJitteredWithinBounds(t *testing.T) {
	backoff := Backoff{Initial: 10 * time.Millisecond, Maximum: 25 * time.Millisecond, Jitter: func(value time.Duration) time.Duration { return value + time.Millisecond }}
	for _, want := range []time.Duration{11, 21, 25, 25} {
		if got := backoff.Next(); got != want*time.Millisecond {
			t.Fatalf("backoff=%s want=%s", got, want*time.Millisecond)
		}
	}
}

func TestDuplicateLinkArbitrationKeepsPreferredInitiator(t *testing.T) {
	local, peer := NodeID{2}, NodeID{1}
	registry := NewDuplicateLinkRegistry(local)
	first, second := &fakeRegistryLink{}, &fakeRegistryLink{}
	if !registry.Register(peer, local, [32]byte{2}, first) {
		t.Fatal("first candidate not registered")
	}
	if !registry.Register(peer, peer, [32]byte{9}, second) {
		t.Fatal("preferred peer-initiated link did not replace current")
	}
	if !first.closed.Load() || second.closed.Load() {
		t.Fatalf("wrong duplicate winner: first closed=%v second closed=%v", first.closed.Load(), second.closed.Load())
	}
	third := &fakeRegistryLink{}
	if registry.Register(peer, peer, [32]byte{1}, third) != true || !second.closed.Load() || third.closed.Load() {
		t.Fatal("same-direction duplicate did not converge on the lower transcript rank")
	}
	fourth := &fakeRegistryLink{}
	if registry.Register(peer, local, [32]byte{0}, fourth) || !fourth.closed.Load() || third.closed.Load() {
		t.Fatal("nonpreferred duplicate was not closed")
	}
	registry.Remove(peer, third)
}

type fakePooledLink struct {
	id        int32
	unhealthy atomic.Bool
	closed    atomic.Bool
}

func (l *fakePooledLink) Healthy() bool { return !l.closed.Load() && !l.unhealthy.Load() }
func (l *fakePooledLink) Close() error  { l.closed.Store(true); return nil }

type fakeRegistryLink struct{ closed atomic.Bool }

func (l *fakeRegistryLink) Close(error) { l.closed.Store(true) }

func TestReconcileEvictsOldestIdleLinksAfterReorderedUse(t *testing.T) {
	links := []*fakePooledLink{{}, {}, {}, {}}
	now := time.Unix(100, 0)
	pool, err := NewLinkPool(PoolConfig{MinLinks: 1, MaxLinks: 4, HardCap: 4}, func(context.Context) (PooledLink, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Oldest-first order differs from storage order. The final entry is active
	// and must survive even though its last-use timestamp is oldest.
	for i, age := range []int{2, 3, 1, 0} {
		pool.entries = append(pool.entries, &poolEntry{link: links[i], lastUsed: now.Add(time.Duration(age) * time.Second)})
	}
	pool.entries[3].active = 1
	pool.Reconcile(0, true)
	if len(pool.entries) != 1 || pool.entries[0].link != links[3] || links[3].closed.Load() {
		t.Fatal("reconcile removed active lease or retained idle links")
	}
	for _, l := range links[:3] {
		if !l.closed.Load() {
			t.Fatal("idle link was not closed")
		}
	}
}
