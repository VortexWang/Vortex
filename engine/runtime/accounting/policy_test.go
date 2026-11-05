package accounting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	core "vortex/engine/accounting"
)

type testPolicyClock struct {
	mu      sync.Mutex
	bounds  TimeBounds
	waiting chan struct{}
	step    chan struct{}
}

func newTestPolicyClock(bounds TimeBounds) *testPolicyClock {
	return &testPolicyClock{bounds: bounds, waiting: make(chan struct{}, 32), step: make(chan struct{}, 32)}
}
func (c *testPolicyClock) Bounds() TimeBounds    { c.mu.Lock(); defer c.mu.Unlock(); return c.bounds }
func (c *testPolicyClock) Set(bounds TimeBounds) { c.mu.Lock(); c.bounds = bounds; c.mu.Unlock() }
func (c *testPolicyClock) Wait(ctx context.Context, _ time.Duration) error {
	select {
	case c.waiting <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.step:
		return nil
	}
}
func (c *testPolicyClock) Advance(bounds TimeBounds) { c.Set(bounds); c.step <- struct{}{} }

func policyFixture(t *testing.T, bounds TimeBounds) (*PolicyBoundary, *CreditBook, *testPolicyClock, PolicyWindow) {
	t.Helper()
	cfg := bookFixture(t, 512<<10)
	next := cfg.Snapshots["new"]
	next.Binding.PolicyRevision = 2
	cfg.Snapshots["new"] = next
	book, err := NewCreditBook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	boundaryAt := time.Unix(1_800_000_000, 0)
	w := PolicyWindow{OldSnapshot: "old", NewSnapshot: "new", At: boundaryAt, PreparedAt: boundaryAt.Add(-60 * time.Second), ExpiresAt: boundaryAt.Add(240 * time.Second), PolicyLeaseUntil: boundaryAt, RuntimeLeaseUntil: boundaryAt, ShareLeaseUntil: boundaryAt, HardDeadline: boundaryAt.Add(time.Hour), SelectionRevision: 2}
	clock := newTestPolicyClock(bounds)
	p, err := NewPolicyBoundary(book, clock, w)
	if err != nil {
		t.Fatal(err)
	}
	return p, book, clock, w
}

func TestPolicyWindowRequiresPreissueAndLeaseCoverage(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	w := PolicyWindow{OldSnapshot: "old", NewSnapshot: "new", At: base, PreparedAt: base.Add(-60 * time.Second), ExpiresAt: base.Add(240 * time.Second), PolicyLeaseUntil: base, RuntimeLeaseUntil: base, ShareLeaseUntil: base, HardDeadline: base.Add(time.Hour), SelectionRevision: 2}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	w.PreparedAt = base.Add(-59 * time.Second)
	if w.Validate() == nil {
		t.Fatal("late preissue accepted")
	}
	w.PreparedAt = base.Add(-60 * time.Second)
	w.RuntimeLeaseUntil = base.Add(-time.Nanosecond)
	if w.Validate() == nil {
		t.Fatal("runtime lease gap accepted")
	}
}

func TestPolicyBoundarySwitchesAtTAndRejectsMissingSnapshot(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	p, book, _, w := policyFixture(t, TimeBounds{Earliest: base.Add(-time.Second), Latest: base.Add(-time.Second)})
	if _, err := p.Open(context.Background(), core.Flow{Key: "before"}); err != nil {
		t.Fatal(err)
	}
	if book.State().Active != "old" {
		t.Fatal(book.State())
	}
	p2, book2, _, _ := policyFixture(t, TimeBounds{Earliest: w.At, Latest: w.At})
	if _, err := p2.Open(context.Background(), core.Flow{Key: "at-t"}); err != nil {
		t.Fatal(err)
	}
	if s := book2.State(); s.Active != "new" || s.SelectionRevision != 2 {
		t.Fatal(s)
	}
	w.NewSnapshot = "absent"
	if _, err := NewPolicyBoundary(book2, p2.clock, w); !errors.Is(err, core.ErrSnapshotMismatch) {
		t.Fatalf("missing next snapshot: %v", err)
	}
}

func TestPolicyBoundaryPausesExistingSessionThenResumesSameSession(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	p, book, clock, w := policyFixture(t, TimeBounds{Earliest: base.Add(-time.Second), Latest: base.Add(-time.Second)})
	session, err := p.Open(context.Background(), core.Flow{Key: "long-lived"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(TimeBounds{Earliest: w.At.Add(-5 * time.Second), Latest: w.At.Add(5 * time.Second)})
	if _, err := p.Open(context.Background(), core.Flow{Key: "during-uncertainty"}); !errors.Is(err, ErrPolicyPaused) {
		t.Fatalf("new flow during uncertainty: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		batch, e := session.Reserve(context.Background(), core.LeftToRight, 1)
		if e == nil {
			e = batch.Commit(context.Background(), 1)
		}
		result <- e
	}()
	<-clock.waiting
	clock.Advance(TimeBounds{Earliest: w.At.Add(time.Second), Latest: w.At.Add(time.Second)})
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if s := book.State(); s.Active != "new" || s.Snapshots["old"].Credit.Used != 0 || s.Snapshots["new"].Credit.Used != 1 {
		t.Fatalf("existing session did not switch policy snapshot: %+v", s)
	}
	_ = session.Close()
}

func TestPolicyBoundaryRejectsExcessUncertaintyAndHardDeadline(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	p, _, clock, w := policyFixture(t, TimeBounds{Earliest: base.Add(-time.Second), Latest: base.Add(-time.Second)})
	clock.Set(TimeBounds{Earliest: w.At.Add(-6 * time.Second), Latest: w.At.Add(6 * time.Second)})
	if _, err := p.Open(context.Background(), core.Flow{Key: "wide"}); !errors.Is(err, ErrPolicyUncertain) {
		t.Fatalf("uncertainty >10s accepted: %v", err)
	}
	clock.Set(TimeBounds{Earliest: w.HardDeadline, Latest: w.HardDeadline})
	if _, err := p.Open(context.Background(), core.Flow{Key: "after-hard-cutoff"}); !errors.Is(err, ErrPolicyExpired) {
		t.Fatalf("hard deadline extended: %v", err)
	}
}

func TestPolicyBoundaryRecoversWithinTwelveSecondProtectionBand(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	p, _, clock, w := policyFixture(t, TimeBounds{Earliest: base.Add(-time.Second), Latest: base.Add(-time.Second)})
	session, err := p.Open(context.Background(), core.Flow{Key: "pause-limit"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(TimeBounds{Earliest: w.At.Add(-5 * time.Second), Latest: w.At.Add(5 * time.Second)})
	result := make(chan error, 1)
	go func() { _, e := session.Reserve(context.Background(), core.LeftToRight, 1); result <- e }()
	<-clock.waiting
	clock.Advance(TimeBounds{Earliest: w.At.Add(-time.Second), Latest: w.At.Add(9 * time.Second)})
	<-clock.waiting
	clock.Advance(TimeBounds{Earliest: w.At, Latest: w.At.Add(10 * time.Second)})
	if err := <-result; err != nil {
		t.Fatalf("policy pause failed to recover within 12 seconds: %v", err)
	}
	_ = session.Close()
}

func TestPolicyBoundaryDoesNotReleaseOldUnknownLiability(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	p, book, clock, w := policyFixture(t, TimeBounds{Earliest: base.Add(-time.Second), Latest: base.Add(-time.Second)})
	session, err := p.Open(context.Background(), core.Flow{Key: "unknown-write"})
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := session.Reserve(context.Background(), core.LeftToRight, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(TimeBounds{Earliest: w.At.Add(-5 * time.Second), Latest: w.At.Add(5 * time.Second)})
	result := make(chan error, 1)
	go func() { result <- bounds.MarkUnknown(context.Background()) }()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if state := book.State(); state.Snapshots["old"].Credit.Unknown != 64<<10 || state.Snapshots["new"].Credit.Unknown != 0 {
		t.Fatalf("policy boundary moved unknown liability: %+v", state.Snapshots)
	}
	_ = session.Close()
}

func TestPolicyTransitionPreservesBothPathsAndOldUnknownLiability(t *testing.T) {
	cfg := bookFixture(t, 4<<20)
	cfg.Snapshots = make(map[string]CreditVersion)
	for _, path := range []uint64{4, 5} {
		for _, policy := range []uint64{1, 2} {
			id := fmt.Sprintf("path-%d-policy-%d", path, policy)
			cfg.Snapshots[id] = CreditVersion{Binding: Binding{RuleID: "rule", ExecutionNode: "node", PathGeneration: path, PolicyRevision: policy, CoefficientMilli: path * 1000}, Credit: CreditConfig{SnapshotID: id, Budget: 128 << 10, InitialBlock: 64 << 10, MaxBlock: 64 << 10, GrantTarget: 64 << 10, RenewPercent: 30}}
		}
	}
	cfg.Active = "path-4-policy-1"
	book, err := NewCreditBook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1800000000, 0)
	clock := newTestPolicyClock(TimeBounds{Earliest: at.Add(-time.Second), Latest: at.Add(-time.Second)})
	window := PolicyWindow{OldSnapshot: "path-4-policy-1", NewSnapshot: "path-4-policy-2", At: at, PreparedAt: at.Add(-60 * time.Second), ExpiresAt: at.Add(240 * time.Second), PolicyLeaseUntil: at, RuntimeLeaseUntil: at, ShareLeaseUntil: at, HardDeadline: at.Add(time.Hour), SelectionRevision: 3}
	boundary, err := NewPolicyBoundary(book, clock, window)
	if err != nil {
		t.Fatal(err)
	}
	oldPath, err := boundary.Open(context.Background(), core.Flow{Key: "old-path-tcp"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := oldPath.Reserve(context.Background(), core.LeftToRight, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Select("path-5-policy-1", 2); err != nil {
		t.Fatal(err)
	}
	newPath, err := boundary.Open(context.Background(), core.Flow{Key: "new-path-udp"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(TimeBounds{Earliest: at, Latest: at})
	for _, session := range []core.Session{oldPath, newPath} {
		batch, err := session.Reserve(context.Background(), core.LeftToRight, 100)
		if err != nil {
			t.Fatal(err)
		}
		if err := batch.Commit(context.Background(), 100); err != nil {
			t.Fatal(err)
		}
		session.Close()
	}
	if err := pending.MarkUnknown(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := book.State()
	if state.Active != "path-5-policy-2" {
		t.Fatalf("policy switch changed active path: %s", state.Active)
	}
	if state.Snapshots["path-4-policy-1"].Credit.Unknown != 100 || state.Snapshots["path-4-policy-1"].Credit.Used != 0 || state.Snapshots["path-5-policy-1"].Credit.Used != 0 {
		t.Fatalf("old policies consumed or released: %+v", state)
	}
	for _, id := range []string{"path-4-policy-2", "path-5-policy-2"} {
		if state.Snapshots[id].Credit.Used != 100 {
			t.Fatalf("path/policy isolation failed: %+v", state)
		}
	}
	for id, snapshot := range state.Snapshots {
		s := snapshot.Credit
		if s.Budget != s.Unissued+s.PendingGrant+s.UnknownGrant+s.Available+s.Held+s.Used+s.Unknown || s.Sessions != 0 || s.Held != 0 {
			t.Fatalf("snapshot %s unconserved: %+v", id, s)
		}
	}
}
