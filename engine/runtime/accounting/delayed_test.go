package accounting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	core "vortex/engine/accounting"
)

func delayedFixture(t *testing.T, delay time.Duration) *DelayedCreditGate {
	t.Helper()
	g, err := NewDelayedCreditGate(context.Background(), CreditConfig{SnapshotID: "s", Budget: 256, InitialBlock: 1, MaxBlock: 1, GrantTarget: 128, RenewPercent: 30}, delay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}
func TestDelayedIssuerCoalescesBeforeWrites(t *testing.T) {
	g := delayedFixture(t, 40*time.Millisecond)
	ctx := context.Background()
	s, err := g.Open(ctx, core.Flow{Key: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if g.Credit.State().Available != 0 || g.Credit.State().PendingGrant != 128 {
		t.Fatal(g.Credit.State())
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := s.Reserve(ctx, core.LeftToRight, 1)
			if err != nil {
				t.Error(err)
				return
			}
			if err = b.Commit(ctx, 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state := g.Credit.State()
	if state.Denials != 0 || state.Waits == nil || state.Waits.Attempts != 16 || state.Waits.FinalFailures != 0 || state.Waits.Waits != state.Waits.WaitSucceeded {
		t.Fatalf("successful waits counted as denials: %+v", state)
	}
	if state.Used != 16 || state.Grants != 1 || state.GrantRecords != 1 || state.PendingGrant != 0 {
		t.Fatal(state)
	}
	conserved(t, g.Credit)
}

func TestFaultedRenewalUnknownCannotBeReplacedOrSpent(t *testing.T) {
	ctx := context.Background()
	g, err := NewFaultedRenewalCreditGate(ctx, CreditConfig{SnapshotID: "s", Budget: 256, InitialBlock: 1, MaxBlock: 1, GrantTarget: 128, RenewPercent: 30}, 40*time.Millisecond, GrantUnknown)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	s, err := g.Open(ctx, core.Flow{Key: "unknown-renewal"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for range 128 {
		b, e := s.Reserve(ctx, core.LeftToRight, 1)
		if e != nil {
			t.Fatal(e)
		}
		if e = b.Commit(ctx, 1); e != nil {
			t.Fatal(e)
		}
	}
	if _, err := s.Reserve(ctx, core.LeftToRight, 1); err == nil {
		t.Fatal("unknown renewal spent")
	}
	before := g.Credit.State()
	if before.UnknownGrant != 90 || before.PendingGrant != 0 || before.Used != 128 || before.Grants != 1 {
		t.Fatalf("unexpected frozen state: %+v", before)
	}
	for range 16 {
		if _, err := s.Reserve(ctx, core.LeftToRight, 1); err == nil {
			t.Fatal("replacement authorized")
		}
	}
	g.Close()
	after := g.Credit.State()
	if after.UnknownGrant != before.UnknownGrant || after.GrantRecords != before.GrantRecords || after.Unissued != before.Unissued {
		t.Fatalf("unknown released/replaced: %+v", after)
	}
	conserved(t, g.Credit)
}

func TestFaultedRenewalRejectedIsDefinitiveAndConserved(t *testing.T) {
	ctx := context.Background()
	g, err := NewFaultedRenewalCreditGate(ctx, CreditConfig{SnapshotID: "s", Budget: 256, InitialBlock: 1, MaxBlock: 1, GrantTarget: 128, RenewPercent: 30}, 40*time.Millisecond, GrantRejected)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	s, err := g.Open(ctx, core.Flow{Key: "rejected-renewal"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for range 128 {
		b, e := s.Reserve(ctx, core.LeftToRight, 1)
		if e != nil {
			t.Fatal(e)
		}
		if e = b.Commit(ctx, 1); e != nil {
			t.Fatal(e)
		}
	}
	before := g.Credit.State()
	if _, err = s.Reserve(ctx, core.LeftToRight, 1); !errors.Is(err, core.ErrDenied) {
		t.Fatalf("rejected renewal was not denied: %v", err)
	}
	state := g.Credit.State()
	if state.PendingGrant != 0 || state.UnknownGrant != 0 || state.Available != 0 || state.Used != 128 || state.Unissued != 128 || state.Grants != 1 || state.GrantRecords != 2 {
		t.Fatalf("rejected renewal changed authority unexpectedly: %+v", state)
	}
	if state.Waits == nil || before.Waits == nil || state.Waits.Waits != before.Waits.Waits+1 || state.Waits.WaitSucceeded != before.Waits.WaitSucceeded || state.Waits.TimedOut != before.Waits.TimedOut || state.Waits.FinalFailures != before.Waits.FinalFailures+1 || state.Waits.Shortfalls != before.Waits.Shortfalls+1 {
		t.Fatalf("rejected renewal wait accounting: %+v", state.Waits)
	}
	conserved(t, g.Credit)
}

func TestDelayedIssuerTimeoutDoesNotReleasePending(t *testing.T) {
	g := delayedFixture(t, time.Second)
	ctx := context.Background()
	s, err := g.Open(ctx, core.Flow{Key: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now()
	_, err = s.Reserve(ctx, core.LeftToRight, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 700*time.Millisecond {
		t.Fatalf("wait=%s", elapsed)
	}
	state := g.Credit.State()
	if state.Denials != 1 || state.Waits == nil || state.Waits.TimedOut != 1 || state.Waits.FinalFailures != 1 || state.Waits.WaitSucceeded != 0 {
		t.Fatalf("timeout counter: %+v", state)
	}
	if state.PendingGrant != 128 || state.Used != 0 || state.Available != 0 {
		t.Fatal(state)
	}
	g.Close()
	state = g.Credit.State()
	if state.UnknownGrant != 128 || state.PendingGrant != 0 || state.Unissued != 128 {
		t.Fatal(state)
	}
	if _, err = s.Reserve(ctx, core.LeftToRight, 1); err == nil {
		t.Fatal("closed issuer accepted reserve")
	}
	conserved(t, g.Credit)
}
func TestDelayedIssuerCallerCancellationKeepsRequest(t *testing.T) {
	g := delayedFixture(t, 40*time.Millisecond)
	ctx := context.Background()
	s, err := g.Open(ctx, core.Flow{Key: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Reserve(canceled, core.LeftToRight, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b, err := s.Reserve(ctx, core.LeftToRight, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Commit(ctx, 1); err != nil {
		t.Fatal(err)
	}
	state := g.Credit.State()
	if state.Waits == nil || state.Waits.Attempts != 2 || state.Waits.Cancelled != 1 || state.Denials != 1 {
		t.Fatalf("cancellation counter: %+v", state)
	}
	if g.Credit.State().Grants != 1 || g.Credit.State().GrantRecords != 1 {
		t.Fatal(g.Credit.State())
	}
	conserved(t, g.Credit)
}

func TestDelayedRenewalSeparatesColdStartFromRenewalDeadline(t *testing.T) {
	ctx := context.Background()
	g, err := NewDelayedRenewalCreditGate(ctx, CreditConfig{SnapshotID: "s", Budget: 256, InitialBlock: 1, MaxBlock: 1, GrantTarget: 128, RenewPercent: 30}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	s, err := g.Open(ctx, core.Flow{Key: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 128; i++ {
		b, e := s.Reserve(ctx, core.LeftToRight, 1)
		if e != nil {
			t.Fatalf("initial grant byte %d: %v", i, e)
		}
		if e = b.Commit(ctx, 1); e != nil {
			t.Fatal(e)
		}
	}
	_, err = s.Reserve(ctx, core.LeftToRight, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("renewal bypassed wait limit: %v", err)
	}
	state := g.Credit.State()
	if state.Grants != 1 || state.Used != 128 || state.PendingGrant != 90 || state.Waits.TimedOut != 1 {
		t.Fatal(state)
	}
	g.Close()
	conserved(t, g.Credit)
	if g.Credit.State().UnknownGrant != 90 {
		t.Fatal("shutdown lost pending renewal", g.Credit.State())
	}
}
