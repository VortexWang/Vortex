package accounting

import (
	"context"
	"testing"

	core "vortex/engine/accounting"
)

func bookFixture(t *testing.T, funding int64) BookConfig {
	t.Helper()
	version := func(id string, coefficient uint64) CreditVersion {
		return CreditVersion{
			Binding: Binding{RuleID: "rule", ExecutionNode: "node", PathGeneration: 4, PolicyRevision: 1, CoefficientMilli: coefficient},
			Credit:  CreditConfig{SnapshotID: id, Budget: 128 << 10, InitialBlock: 64 << 10, MaxBlock: 64 << 10, GrantTarget: 64 << 10, RenewPercent: 30},
		}
	}
	return BookConfig{FundingBytes: funding, Active: "old", SelectionRevision: 1, Snapshots: map[string]CreditVersion{
		"old": version("old", 1000), "new": version("new", 3000),
	}}
}

func TestCreditBookSelectionPinsExistingSessions(t *testing.T) {
	ctx := context.Background()
	cfg := bookFixture(t, 512<<10)
	book, err := NewCreditBook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldSession, err := book.Open(ctx, core.Flow{Key: "old-flow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Select("new", 2); err != nil {
		t.Fatal(err)
	}
	newSession, err := book.Open(ctx, core.Flow{Key: "new-flow"})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []core.Session{oldSession, newSession} {
		batch, err := session.Reserve(ctx, core.LeftToRight, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if err := batch.Commit(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
	}
	state := book.State()
	if state.Active != "new" || state.SelectionRevision != 2 || state.Snapshots["old"].Credit.Used != 1000 || state.Snapshots["new"].Credit.Used != 1000 {
		t.Fatalf("session snapshot affinity lost: %+v", state)
	}
	if state.Snapshots["old"].UsedUpper != 1000 || state.Snapshots["new"].UsedUpper != 3000 {
		t.Fatalf("coefficient binding mismatch: %+v", state.Snapshots)
	}
	if err := book.Select("new", 2); err != nil {
		t.Fatal("identical selection must be idempotent:", err)
	}
	if err := book.Select("old", 2); err == nil {
		t.Fatal("same revision changed active snapshot")
	}
	if err := book.Select("missing", 3); err == nil {
		t.Fatal("un-pinned snapshot selected")
	}
}

func TestCreditBookPreallocatesFundingAndCopiesBindings(t *testing.T) {
	cfg := bookFixture(t, 512<<10)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := bookFixture(t, (512<<10)-1).Validate(); err == nil {
		t.Fatal("underfunded snapshot book accepted")
	}
	book, err := NewCreditBook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Snapshots["old"] = CreditVersion{}
	if book.State().Snapshots["old"].Binding.CoefficientMilli != 1000 {
		t.Fatal("caller mutated pinned binding")
	}
	if got := BilledUpper(1001, 1001); got != 1003 {
		t.Fatalf("unexpected single-ceiling upper bound: %d", got)
	}
}

func TestCreditBookLateGrantStaysWithOldSnapshot(t *testing.T) {
	ctx := context.Background()
	book, err := NewCreditBook(bookFixture(t, 512<<10))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.BeginGrant(ctx, "old", "old-request", 64<<10); err != nil {
		t.Fatal(err)
	}
	if err := book.ApplyGrant(ctx, GrantReply{RequestID: "old-request", SnapshotID: "old", Amount: 64 << 10, Outcome: GrantUnknown}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := book.Select("new", 2); err != nil {
		t.Fatal(err)
	}
	before := book.State().Snapshots["new"].Credit
	late := GrantReply{RequestID: "old-request", SnapshotID: "old", GrantID: "global-grant", Amount: 64 << 10, Outcome: GrantAccepted}
	if err := book.ApplyGrant(ctx, late, 3000); err == nil {
		t.Fatal("late grant accepted with new snapshot coefficient")
	}
	if err := book.ApplyGrant(ctx, late, 1000); err != nil {
		t.Fatal(err)
	}
	state := book.State()
	if state.Snapshots["old"].Credit.Available != 64<<10 || state.Snapshots["new"].Credit != before {
		t.Fatalf("late grant crossed snapshot boundary: %+v", state.Snapshots)
	}
	if err := book.BeginGrant(ctx, "new", "new-request", 64<<10); err != nil {
		t.Fatal(err)
	}
	reused := GrantReply{RequestID: "new-request", SnapshotID: "new", GrantID: "global-grant", Amount: 64 << 10, Outcome: GrantAccepted}
	if err := book.ApplyGrant(ctx, reused, 3000); err == nil {
		t.Fatal("grant identity reused across snapshots")
	}
	if state = book.State(); state.Snapshots["old"].Credit.Available != 64<<10 || state.Snapshots["new"].Credit.PendingGrant != 64<<10 {
		t.Fatalf("conflicting grant changed ledger: %+v", state.Snapshots)
	}
}

func TestCreditBookSelectionNeverReleasesUnknownLiability(t *testing.T) {
	ctx := context.Background()
	book, err := NewCreditBook(bookFixture(t, 512<<10))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.BeginGrant(ctx, "old", "unknown", 64<<10); err != nil {
		t.Fatal(err)
	}
	if err := book.ApplyGrant(ctx, GrantReply{RequestID: "unknown", SnapshotID: "old", Amount: 64 << 10, Outcome: GrantUnknown}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := book.Select("new", 2); err != nil {
		t.Fatal(err)
	}
	if got := book.State().Snapshots["old"].Credit.UnknownGrant; got != 64<<10 {
		t.Fatalf("snapshot change released unknown grant: %d", got)
	}
}

func TestPolicyLookupRejectsMissingOrAmbiguousPathAuthority(t *testing.T) {
	cfg := bookFixture(t, 512<<10)
	book, err := NewCreditBook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	binding := cfg.Snapshots["old"].Binding
	if _, _, err := book.openPolicy(context.Background(), core.Flow{Key: "missing"}, binding, 2); err == nil {
		t.Fatal("missing policy authority accepted")
	}
	if _, _, err := book.openPolicy(context.Background(), core.Flow{Key: "ambiguous"}, binding, 1); err == nil {
		t.Fatal("ambiguous policy authority accepted")
	}
	for _, state := range book.State().Snapshots {
		if state.Credit.Sessions != 0 || state.Credit.Used != 0 {
			t.Fatal("failed lookup consumed authority")
		}
	}
}
