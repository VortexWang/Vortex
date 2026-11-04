package share

import (
	"sync"
	"testing"
)

func shareKey(node, boot byte) Key { var k Key; k.Node[0] = node; k.Boot[0] = boot; return k }

func TestPreparePreservesActiveAndCASCommitFencesStaleRevision(t *testing.T) {
	m := NewManager()
	k := shareKey(1, 1)
	a, e := m.Prepare(k, 50, 50)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Commit(k, a.Generation); e != nil {
		t.Fatal(e)
	}
	b, e := m.Prepare(k, 60, 60)
	if e != nil {
		t.Fatal(e)
	}
	if active, ok := m.Get(k); !ok || !active.Active || active.Generation != a.Generation {
		t.Fatalf("prepare replaced active: %+v %v", active, ok)
	}
	c, e := m.Prepare(k, 40, 40)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Commit(k, b.Generation); e != ErrConflict {
		t.Fatalf("stale prepare committed: %v", e)
	}
	active, e := m.Commit(k, c.Generation)
	if e != nil || !active.Active {
		t.Fatalf("commit=%+v err=%v", active, e)
	}
	if replay, e := m.Commit(k, c.Generation); e != nil || !replay.Active {
		t.Fatalf("commit replay=%+v err=%v", replay, e)
	}
	var old Revision
	for _, r := range m.Revisions(k) {
		if r.Generation == a.Generation {
			old = r
		}
	}
	if !old.RenewalClosed || old.Draining {
		t.Fatalf("old revision not draining: %+v", old)
	}
}

func TestPrepareWithIDIsIdempotentAndRejectsIdentityReuse(t *testing.T) {
	m := NewManager()
	k := shareKey(1, 2)
	var tx [16]byte
	tx[0] = 1
	a, e := m.PrepareWithID(k, tx, 100, 5)
	if e != nil {
		t.Fatal(e)
	}
	b, e := m.PrepareWithID(k, tx, 100, 5)
	if e != nil || a.Generation != b.Generation {
		t.Fatalf("replay=%+v err=%v", b, e)
	}
	if _, e = m.PrepareWithID(k, tx, 101, 5); e != ErrConflict {
		t.Fatalf("identity reuse accepted: %v", e)
	}
}

func TestOverlapOccupancyUsesMaximumAndDrainsOldLeases(t *testing.T) {
	m := NewManager()
	k := shareKey(1, 3)
	old, _ := m.Prepare(k, 1, 50)
	_, _ = m.Commit(k, old.Generation)
	oldLeases := make([]*Lease, 50)
	for i := range oldLeases {
		var e error
		oldLeases[i], e = m.Acquire(k)
		if e != nil {
			t.Fatal(i, e)
		}
	}
	newRev, _ := m.Prepare(k, 1, 60)
	_, e := m.Commit(k, newRev.Generation)
	if e != nil {
		t.Fatal(e)
	}
	third, _ := m.Prepare(k, 1, 40)
	if _, e := m.Commit(k, third.Generation); e != ErrDrain {
		t.Fatalf("third generation activated before prior drain: %v", e)
	}
	newLeases := make([]*Lease, 10)
	for i := range newLeases {
		var err error
		newLeases[i], err = m.Acquire(k)
		if err != nil {
			t.Fatalf("overlap cap rejected lease %d: %v", i, err)
		}
	}
	if _, e = m.Acquire(k); e != ErrCapacity {
		t.Fatalf("occupancy exceeded max(old,new): %v", e)
	}
	for i := 0; i < 10; i++ {
		oldLeases[i].Close()
	}
	for i := 0; i < 10; i++ {
		if _, e = m.Acquire(k); e != nil {
			t.Fatalf("released occupancy was not reusable: %v", e)
		}
	}
	if _, e = m.Acquire(k); e != ErrCapacity {
		t.Fatalf("drain exceeded overlap limit: %v", e)
	}
	for i := 10; i < 50; i++ {
		oldLeases[i].Close()
	}
	for i := 0; i < 50; i++ {
		l, err := m.Acquire(k)
		if err != nil {
			t.Fatalf("new share failed after old drain at %d: %v", i, err)
		}
		l.Close()
	}
	for _, l := range newLeases {
		l.Close()
	}
	var oldState Revision
	for _, r := range m.Revisions(k) {
		if r.Generation == old.Generation {
			oldState = r
		}
	}
	if oldState.InUse != 0 || oldState.Draining {
		t.Fatalf("old share did not fully drain: %+v", oldState)
	}
}

func TestDowngradeUsesOverlapMaximumButStopsAtNewCap(t *testing.T) {
	m := NewManager()
	k := shareKey(2, 1)
	old, _ := m.Prepare(k, 1, 60)
	_, _ = m.Commit(k, old.Generation)
	leases := make([]*Lease, 50)
	for i := range leases {
		leases[i], _ = m.Acquire(k)
	}
	next, _ := m.Prepare(k, 1, 40)
	if _, e := m.Commit(k, next.Generation); e != nil {
		t.Fatal(e)
	}
	newLeases := make([]*Lease, 10)
	for i := 0; i < 10; i++ {
		var err error
		if newLeases[i], err = m.Acquire(k); err != nil {
			t.Fatalf("max overlap should allow 10 new leases: %v", err)
		}
	}
	if _, e := m.Acquire(k); e != ErrCapacity {
		t.Fatalf("aggregate max exceeded: %v", e)
	}
	for _, l := range leases {
		l.Close()
	}
	for _, l := range newLeases {
		l.Close()
	}
	newLeases = make([]*Lease, 40)
	for i := 0; i < 40; i++ {
		l, e := m.Acquire(k)
		if e != nil {
			t.Fatalf("new allocation %d: %v", i, e)
		}
		newLeases[i] = l
	}
	if _, e := m.Acquire(k); e != ErrCapacity {
		t.Fatalf("new generation cap exceeded: %v", e)
	}
	for _, l := range newLeases {
		l.Close()
	}
}

func TestSequentialUpgradeAndDowngradeAfterDrain(t *testing.T) {
	m := NewManager()
	k := shareKey(8, 1)
	first, _ := m.Prepare(k, 1, 50)
	_, _ = m.Commit(k, first.Generation)
	oldLease, _ := m.Acquire(k)
	second, _ := m.Prepare(k, 1, 60)
	if _, err := m.Commit(k, second.Generation); err != nil {
		t.Fatal(err)
	}
	oldLease.Close()
	for _, revision := range m.Revisions(k) {
		if revision.Generation == first.Generation && revision.Draining {
			t.Fatal("old generation did not finish draining")
		}
	}
	leases := make([]*Lease, 60)
	for i := range leases {
		var err error
		leases[i], err = m.Acquire(k)
		if err != nil {
			t.Fatalf("upgrade capacity at %d: %v", i, err)
		}
	}
	third, _ := m.Prepare(k, 1, 40)
	if _, err := m.Commit(k, third.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(k); err != ErrCapacity {
		t.Fatalf("triple-generation occupancy escaped bound: %v", err)
	}
	for _, lease := range leases {
		lease.Close()
	}
	current := make([]*Lease, 40)
	for i := range current {
		var err error
		current[i], err = m.Acquire(k)
		if err != nil {
			t.Fatalf("downgrade capacity at %d: %v", i, err)
		}
	}
	if _, err := m.Acquire(k); err != ErrCapacity {
		t.Fatalf("downgraded cap exceeded: %v", err)
	}
	for _, lease := range current {
		lease.Close()
	}
}

func TestBootFenceAndOldRenewalClosure(t *testing.T) {
	m := NewManager()
	oldKey := shareKey(7, 1)
	old, _ := m.Prepare(oldKey, 1, 2)
	_, _ = m.Commit(oldKey, old.Generation)
	if e := m.CloseRenewal(oldKey, old.Generation); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Acquire(oldKey); e != ErrPrepared {
		t.Fatalf("closed renewal admitted: %v", e)
	}
	newKey := shareKey(7, 2)
	if e := m.BeginBoot(newKey); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Prepare(oldKey, 1, 2); e != ErrConflict {
		t.Fatalf("old boot prepared after new boot: %v", e)
	}
	if _, e := m.Acquire(oldKey); e != ErrConflict {
		t.Fatalf("old boot admitted after new boot: %v", e)
	}
	newRev, e := m.Prepare(newKey, 1, 2)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Commit(newKey, newRev.Generation); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentAcquireAndReleaseConservesCapacity(t *testing.T) {
	m := NewManager()
	k := shareKey(3, 4)
	r, _ := m.Prepare(k, 1, 8)
	_, _ = m.Commit(k, r.Generation)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, e := m.Acquire(k); e == nil {
				l.Close()
				l.Close()
			}
		}()
	}
	wg.Wait()
	state, _ := m.Get(k)
	if state.InUse != 0 {
		t.Fatalf("leaked share leases: %+v", state)
	}
}

func TestRevisionHistoryRemainsBounded(t *testing.T) {
	m := NewManager()
	k := shareKey(9, 1)
	for i := 0; i < maxRevisionHistory*4; i++ {
		r, err := m.Prepare(k, 1, 1)
		if err != nil {
			t.Fatal(i, err)
		}
		if _, err = m.Commit(k, r.Generation); err != nil {
			t.Fatal(i, err)
		}
	}
	if got := len(m.Revisions(k)); got > maxRevisionHistory {
		t.Fatalf("revision history grew without bound: %d", got)
	}
}
