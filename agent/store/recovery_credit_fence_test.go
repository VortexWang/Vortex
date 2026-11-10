package store

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func creditFenceFixture(t *testing.T) (*Store, RecoveryCreditFence, AuthorizationUpdate, AuthorizationRecord, string) {
	t.Helper()
	s, r, u, path := shareFixture(t)
	ctx := context.Background()
	if e := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); e != nil {
		t.Fatal(e)
	}
	grant := shareGrant(r, u)
	if e := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); e != nil {
		t.Fatal(e)
	}
	p := RecoveryCreditFence{Version: 1, ID: shareID(1200), RecoveryID: shareID(1201), BackendID: shareID(90), HubID: u.IssuerID, NodeID: u.NodeID, BootID: u.BootID, ParentID: grant.ParentAuthorizationID, SnapshotID: grant.BillingSnapshotID, ManifestSHA256: strings.Repeat("a", 64), SealedSequence: 1, IssuedAt: r.NotBefore.Add(5 * time.Second), ExpiresAt: r.NotBefore.Add(4 * time.Minute), FromEpoch: 1, ToEpoch: 2, OriginalRaw: 1024, Issued: 256, Remaining: 768, Root: r, Credits: []RecoveryCreditEntry{{GrantID: grant.AuthorizationID, Amount: 256}}}
	return s, p, u, grant, path
}
func applyFence(t *testing.T, s *Store, p RecoveryCreditFence) (RecoveryCreditFenceReceipt, error) {
	t.Helper()
	raw, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return s.ApplyRecoveryCreditFence(context.Background(), p.NodeID, p.BootID, p.BackendID, raw, IssuerTime{Earliest: p.IssuedAt, Latest: p.IssuedAt, Trusted: true})
}

func TestRecoveryCreditFenceRejectsNodeChildMissingFromHubSnapshot(t *testing.T) {
	s, p, u, grant, _ := creditFenceFixture(t)
	ctx := context.Background()
	// Backend/rolled-back Hub inventory knows only the first256-byte child.
	// A second64-byte original child was FULL-installed at Node but unreported.
	extra := grant
	extra.AuthorizationID = shareID(1202)
	extra.AmountBytes = 64
	if e := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 1203, p.Root), []AuthorizationRecord{extra}, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := applyFence(t, s, p); e == nil {
		t.Fatal("rollback forgot installed Node child and inflated R")
	}
	var count int
	if e := s.db.QueryRow(`SELECT count(*) FROM recovery_credit_fences`).Scan(&count); e != nil || count != 0 {
		t.Fatal("rejected fence persisted", count, e)
	}
	b, e := s.AuditAuthorizationSource(ctx, p.NodeID, p.Root.SourceID)
	if e != nil || b.Initial != 320 || b.Remaining != 320 {
		t.Fatal("rejection changed old funds", b, e)
	}
	p.Credits = append(p.Credits, RecoveryCreditEntry{GrantID: extra.AuthorizationID, Amount: 64})
	p.Issued = 320
	p.Remaining = 704
	receipt, e := applyFence(t, s, p)
	if e != nil || len(receipt.Installed) != 2 {
		t.Fatal(receipt, e)
	}
	delayed := grant
	delayed.AuthorizationID = shareID(1204)
	delayed.AmountBytes = 1
	if e = s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 1205, p.Root), []AuthorizationRecord{delayed}, nil); e == nil {
		t.Fatal("old epoch installed delayed credit after fence")
	}
	use := AuthorizationUse{NodeID: p.NodeID, BootID: p.BootID, AuthorizationID: grant.AuthorizationID, Kind: "credit_grant", RuleID: p.Root.RuleID, RuleRevision: p.Root.RuleRevision, PathGeneration: p.Root.PathGeneration, BillingSnapshotID: p.SnapshotID, Earliest: p.IssuedAt, Latest: p.IssuedAt, TrustedTime: true}
	if _, e = s.CheckStoredAuthorization(ctx, use); e != nil {
		t.Fatal("fence stopped existing rights", e)
	}
}

func TestRecoveryCreditFenceFULLReplayAndExactSuccessorBudget(t *testing.T) {
	s, p, u, grant, path := creditFenceFixture(t)
	ctx := context.Background()
	// One expected old child has never been installed. Its budget still belongs
	// to the original PG inventory and may not become extra fresh R at this Node.
	p.Credits = append(p.Credits, RecoveryCreditEntry{GrantID: shareID(1206), Amount: 256})
	p.Issued = 512
	p.Remaining = 512
	if _, e := s.db.Exec(`CREATE TRIGGER fail_recovery_fence BEFORE INSERT ON recovery_credit_fences BEGIN SELECT RAISE(ABORT,'FULL fence failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := applyFence(t, s, p); e == nil {
		t.Fatal("write failure acknowledged")
	}
	if _, e := s.db.Exec(`DROP TRIGGER fail_recovery_fence`); e != nil {
		t.Fatal(e)
	}
	first, e := applyFence(t, s, p)
	if e != nil || len(first.Installed) != 1 {
		t.Fatal(first, e)
	}
	next := p.Root
	next.Revision++
	next.SignerEpoch++
	if e = s.InstallShareSourceAnchor(ctx, p.NodeID, p.BackendID, next); e != nil {
		t.Fatal(e)
	}
	h := EpochHandoff{TransferID: p.RecoveryID, ParentID: p.ParentID, SourceID: p.Root.SourceID, From: 1, To: 2, Remaining: p.Remaining, SealedSequence: 1, RecordSHA256: p.ManifestSHA256}
	update := shareUpdate(u, 1210, next)
	update.SignerEpoch = 2
	successor := grant
	successor.SignerEpoch = 2
	successor.AuthorizationID = shareID(1211)
	successor.AmountBytes = 400
	bad := h
	bad.Remaining++
	update.Handoffs = []EpochHandoff{bad}
	if e = s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{successor}, nil); e == nil {
		t.Fatal("handoff exceeded backend-bound R")
	}
	bad = h
	bad.RecordSHA256 = strings.Repeat("b", 64)
	update.Handoffs = []EpochHandoff{bad}
	if e = s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{successor}, nil); e == nil {
		t.Fatal("different manifest accepted")
	}
	update.Handoffs = []EpochHandoff{h}
	if e = s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{successor}, nil); e != nil {
		t.Fatal(e)
	}
	successor.AuthorizationID = shareID(1212)
	successor.AmountBytes = 112
	update = shareUpdate(update, 1213, next)
	if e = s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{successor}, nil); e != nil {
		t.Fatal(e)
	}
	// Even a corrupted mutable handoff remainder cannot exceed the independent
	// immutable backend fence (original raw cap alone would leave256 here).
	if _, e = s.db.Exec(`UPDATE authorization_handoffs SET remaining_bytes='999'`); e != nil {
		t.Fatal(e)
	}
	successor.AuthorizationID = shareID(1214)
	successor.AmountBytes = 1
	update = shareUpdate(update, 1215, next)
	if e = s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{successor}, nil); e == nil {
		t.Fatal("mutable R bypassed immutable fence")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	raw, _ := json.Marshal(p)
	replay, e := s.ApplyRecoveryCreditFence(ctx, p.NodeID, p.BootID, p.BackendID, raw, IssuerTime{})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(replay)
	if !bytes.Equal(a, b) {
		t.Fatal("reopen replay changed receipt")
	}
	balance, e := s.AuditAuthorizationSource(ctx, p.NodeID, p.Root.SourceID)
	if e != nil || balance.Initial != 768 || balance.Remaining != 768 {
		t.Fatal(balance, e)
	}
}

func TestRecoveryCreditFenceScopeCanonicalConcurrentAndImmutable(t *testing.T) {
	s, p, _, _, _ := creditFenceFixture(t)
	ctx := context.Background()
	raw, _ := json.Marshal(p)
	if _, e := s.ApplyRecoveryCreditFence(ctx, p.NodeID, shareID(1220), p.BackendID, raw, IssuerTime{}); e == nil {
		t.Fatal("wrong boot")
	}
	if _, e := s.ApplyRecoveryCreditFence(ctx, p.NodeID, p.BootID, shareID(1221), raw, IssuerTime{}); e == nil {
		t.Fatal("wrong backend")
	}
	for _, bad := range [][]byte{append(raw, ' '), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"unknown":1`), 1)} {
		if _, e := DecodeRecoveryCreditFence(bad); e == nil {
			t.Fatal("noncanonical accepted")
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan RecoveryCreditFenceReceipt, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; r, e := applyFence(t, s, p); results <- r; errs <- e }()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []byte
	for r := range results {
		b, _ := json.Marshal(r)
		if first == nil {
			first = b
		}
		if !bytes.Equal(first, b) {
			t.Fatal("split receipt")
		}
	}
	for _, sql := range []string{`DELETE FROM recovery_credit_fences`, `UPDATE recovery_credit_fences SET payload=x'00'`} {
		if _, e := s.db.Exec(sql); e == nil {
			t.Fatal("mutable fence")
		}
	}
	p.Remaining--
	p.Issued++
	p.Credits[0].Amount++
	if _, e := applyFence(t, s, p); e == nil {
		t.Fatal("same ID changed")
	}
}

func TestRecoveryCreditFenceRejectsMissingOrChangedOriginalScope(t *testing.T) {
	for _, name := range []string{"missing-child", "amount", "parent", "source", "snapshot", "root", "expired", "gap", "sum"} {
		t.Run(name, func(t *testing.T) {
			s, p, _, _, _ := creditFenceFixture(t)
			switch name {
			case "missing-child":
				p.Credits = []RecoveryCreditEntry{}
				p.Issued = 0
				p.Remaining = p.OriginalRaw
			case "amount":
				p.Credits[0].Amount--
				p.Issued--
				p.Remaining++
			case "parent":
				p.ParentID = shareID(1222)
			case "source":
				p.Root.SourceID = shareID(1223)
			case "snapshot":
				p.SnapshotID = shareID(1224)
			case "root":
				p.Root.RateBPS++
			case "expired":
				p.ExpiresAt = p.IssuedAt
			case "gap":
				p.ToEpoch++
			case "sum":
				p.Issued++
				p.Remaining--
			}
			sort.Slice(p.Credits, func(i, j int) bool { return p.Credits[i].GrantID < p.Credits[j].GrantID })
			if _, e := applyFence(t, s, p); e == nil {
				t.Fatal("unsafe fence", name)
			}
		})
	}
}

func TestRecoveryCreditFenceRetainsOriginalUnknownLiability(t *testing.T) {
	s, p, _, grant, _ := creditFenceFixture(t)
	ctx := context.Background()
	use := AuthorizationUse{NodeID: p.NodeID, BootID: p.BootID, AuthorizationID: grant.AuthorizationID, Kind: "credit_grant", RuleID: p.Root.RuleID, RuleRevision: p.Root.RuleRevision, PathGeneration: p.Root.PathGeneration, BillingSnapshotID: p.SnapshotID, Earliest: p.IssuedAt, Latest: p.IssuedAt, TrustedTime: true}
	reservation := UsageReservation{NodeID: p.NodeID, BootID: p.BootID, AuthorizationID: grant.AuthorizationID, BatchID: shareID(1230), SnapshotID: p.SnapshotID, FlowKey: "old-flow", RuleID: p.Root.RuleID, Direction: "ingress", Reserved: 64}
	if _, e := s.ReserveAuthorizedUsage(ctx, reservation, use); e != nil {
		t.Fatal(e)
	}
	unknown, e := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: reservation.BatchID, OriginBootId: p.BootID, BillingSnapshotId: p.SnapshotID, GrantId: grant.AuthorizationID, RuleId: p.Root.RuleID, Role: "source", Direction: reservation.Direction, UnknownUpperBound: 64})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SettleUnknownUsageReservation(ctx, reservation, unknown); e != nil {
		t.Fatal(e)
	}
	before, e := s.AuditAuthorizationSource(ctx, p.NodeID, p.Root.SourceID)
	if e != nil || before.Unknown != 64 {
		t.Fatal(before, e)
	}
	if _, e = applyFence(t, s, p); e != nil {
		t.Fatal(e)
	}
	after, e := s.AuditAuthorizationSource(ctx, p.NodeID, p.Root.SourceID)
	if e != nil || after != before {
		t.Fatal("fence released original unknown", before, after, e)
	}
}

func TestRecoveryCreditFenceIndependentConnectionsChooseOneFloor(t *testing.T) {
	s, p, _, _, path := creditFenceFixture(t)
	other, e := Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, db := range []*Store{s, other} {
		wg.Add(1)
		go func(i int, db *Store) {
			defer wg.Done()
			<-start
			c := p
			c.ID = shareID(1240 + i)
			_, e := applyFence(t, db, c)
			errs <- e
		}(i, db)
	}
	close(start)
	wg.Wait()
	close(errs)
	ok := 0
	for e := range errs {
		if e == nil {
			ok++
		}
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM recovery_credit_fences`).Scan(&count); e != nil || count != 1 || ok != 1 {
		t.Fatal("forked floor", count, ok, e)
	}
}

func TestRecoveryCreditFenceRacesLateOldChildOnIndependentConnection(t *testing.T) {
	for i := 0; i < 4; i++ {
		t.Run(shareID(1250+i), func(t *testing.T) {
			s, p, u, grant, path := creditFenceFixture(t)
			other, e := Open(path, "agent")
			if e != nil {
				t.Fatal(e)
			}
			defer other.Close()
			child := grant
			child.AuthorizationID = shareID(1260)
			child.AmountBytes = 64
			start := make(chan struct{})
			errs := make(chan error, 2)
			go func() { <-start; _, e := applyFence(t, other, p); errs <- e }()
			go func() {
				<-start
				errs <- s.PersistAuthorizationUpdate(context.Background(), shareUpdate(u, 1261, p.Root), []AuthorizationRecord{child}, nil)
			}()
			close(start)
			a, b := <-errs, <-errs
			if a == nil && b == nil {
				t.Fatal("fence missed concurrently installed old child")
			}
			var fences, children int
			if e = s.db.QueryRow(`SELECT count(*) FROM recovery_credit_fences`).Scan(&fences); e != nil {
				t.Fatal(e)
			}
			if e = s.db.QueryRow(`SELECT count(*) FROM authorization_records WHERE authorization_id=?`, child.AuthorizationID).Scan(&children); e != nil {
				t.Fatal(e)
			}
			if fences+children != 1 {
				t.Fatal("neither atomic outcome committed", fences, children, a, b)
			}
		})
	}
}
