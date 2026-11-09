package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func issuerFixture(t *testing.T) (*Store, HubDelegation, IssuerTime, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	d := HubDelegation{ID: "550e8400-e29b-41d4-a716-446655440100", HubID: "550e8400-e29b-41d4-a716-446655440101", SourceID: "550e8400-e29b-41d4-a716-446655440102", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440103", ShareSourceID: "550e8400-e29b-41d4-a716-446655440104", NodeID: "550e8400-e29b-41d4-a716-446655440105", BootID: "550e8400-e29b-41d4-a716-446655440106", BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440107", RuleID: "rule", RuleRevision: 2, PathGeneration: 4, FencingFloor: 8, SignerEpoch: 1, RawBytes: 1024, RateBPS: 1000000, Connections: 1, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	clock := IssuerTime{Earliest: now.Add(time.Second), Latest: now.Add(2 * time.Second), Trusted: true}
	if err := s.InstallHubDelegation(context.Background(), d, clock); err != nil {
		t.Fatal(err)
	}
	return s, d, clock, path
}

func issuerRequest(d HubDelegation, n int) IssueRequest {
	return IssueRequest{ID: fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", n), HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, RawBytes: 256, TTL: time.Minute}
}

func TestIssuerPersistsBeforeResponseAndReplaysAfterRestart(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 200)
	first, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 4 || len(first.Digest) != 32 {
		t.Fatalf("result=%+v", first)
	}
	if err := s.InstallHubDelegation(ctx, d, clock); err != nil {
		t.Fatal(err)
	}
	secondStore, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	clock.Earliest = clock.Earliest.Add(45 * time.Second)
	clock.Latest = clock.Latest.Add(45 * time.Second)
	replay, err := secondStore.IssueHubAuthorization(ctx, r, clock)
	if err != nil || !bytes.Equal(first.Payload, replay.Payload) || !bytes.Equal(first.Digest, replay.Digest) {
		t.Fatalf("replay changed: %v", err)
	}
	balance, err := secondStore.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Remaining != 768 || balance.Issued != 256 {
		t.Fatalf("duplicate/refill: %+v %v", balance, err)
	}
	r.RawBytes++
	if _, err := secondStore.IssueHubAuthorization(ctx, r, clock); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("changed request: %v", err)
	}
}

func TestIssuerConcurrentAutonomyConservesSource(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	clock.Earliest = d.BackendHeartbeat.Add(31 * time.Second)
	clock.Latest = clock.Earliest.Add(time.Second)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for n := 200; n < 216; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.IssueHubAuthorization(ctx, issuerRequest(d, n), clock)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if !errors.Is(err, ErrAuthorizationDenied) {
				t.Errorf("issue: %v", err)
			}
		}(n)
	}
	wg.Wait()
	if success != 4 {
		t.Fatalf("issued %d grants, want 4", success)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Initial != 1024 || b.Issued != 1024 || b.Remaining != 0 {
		t.Fatalf("conservation: %+v %v", b, err)
	}
	status, err := s.CurrentAutonomyStatus(ctx, clock.Latest, true)
	if err != nil || status.Mode != AutonomyOffline || status.SignerEpochOpen {
		t.Fatalf("status=%+v %v", status, err)
	}
	// A brand-new backend allocation during loss is not authorized by a node's request.
	d.ID = "550e8400-e29b-41d4-a716-446655440999"
	if err := s.InstallHubDelegation(ctx, d, clock); !errors.Is(err, ErrNewAuthorityInAutonomy) {
		t.Fatalf("new delegation during loss: %v", err)
	}
}

func TestIssuerRefusesExpandedScopeUnknownTimeAndClosedEpoch(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 200)
	for _, change := range []func(*IssueRequest){func(r *IssueRequest) { r.NodeID = "another-node" }, func(r *IssueRequest) { r.BootID = "new-boot" }, func(r *IssueRequest) { r.HubID = "another-hub" }, func(r *IssueRequest) { r.DelegationID = "missing" }, func(r *IssueRequest) { r.TTL = 301 * time.Second }, func(r *IssueRequest) { r.RawBytes = 1025 }} {
		bad := r
		change(&bad)
		if _, err := s.IssueHubAuthorization(ctx, bad, clock); err == nil {
			t.Fatalf("expanded request accepted: %+v", bad)
		}
	}
	uncertain := clock
	uncertain.Trusted = false
	if _, err := s.IssueHubAuthorization(ctx, r, uncertain); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("untrusted clock: %v", err)
	}
	first, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CloseHubSigner(ctx, d.HubID, d.SignerEpoch); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock); !errors.Is(err, ErrSignerEpochClosed) {
		t.Fatalf("closed signer issued: %v", err)
	}
	replay, err := reopened.IssueHubAuthorization(ctx, r, clock)
	if err != nil || !bytes.Equal(first.Payload, replay.Payload) {
		t.Fatalf("old issued result not preserved: %v", err)
	}
	b, err := reopened.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 768 || b.Issued != 256 {
		t.Fatalf("signer closure released reserved bytes: %+v %v", b, err)
	}
	d.ID = "550e8400-e29b-41d4-a716-446655440998"
	if err := reopened.InstallHubDelegation(ctx, d, clock); !errors.Is(err, ErrSignerEpochClosed) {
		t.Fatalf("closed epoch reopened: %v", err)
	}
	d.SignerEpoch++
	if err := reopened.InstallHubDelegation(ctx, d, clock); !errors.Is(err, ErrAuthorityHandoffRequired) {
		t.Fatalf("epoch bypassed handoff: %v", err)
	}
}

func TestIssuerBoundsExpiryAndDoesNotRefundExpiredGrants(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	clock.Earliest = d.ExpiresAt.Add(-3 * time.Second)
	clock.Latest = d.ExpiresAt.Add(-2 * time.Second)
	r := issuerRequest(d, 200)
	r.TTL = 300 * time.Second
	issued, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SignerEpoch    uint64 `json:"signer_epoch"`
		PathGeneration uint64 `json:"path_generation"`
		Authorizations []struct {
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(issued.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.SignerEpoch != 1 || p.PathGeneration != 4 || len(p.Authorizations) != 3 {
		t.Fatalf("epochs/schema: %s", issued.Payload)
	}
	for _, a := range p.Authorizations {
		if !a.ExpiresAt.Equal(d.ExpiresAt) {
			t.Fatalf("expiry extended: %s", issued.Payload)
		}
	}
	clock.Earliest = d.AutonomyDeadline
	clock.Latest = clock.Earliest
	if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock); !errors.Is(err, ErrAuthorityExpired) {
		t.Fatalf("hard deadline: %v", err)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 768 || b.Issued != 256 {
		t.Fatalf("expiry released reserve: %+v %v", b, err)
	}
}

func TestIssuerCommitFailureDoesNotConsumeOrRespond(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_issued BEFORE INSERT ON issuer_issues BEGIN SELECT RAISE(ABORT,'disk fixture'); END`); err != nil {
		t.Fatal(err)
	}
	response, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 200), clock)
	if err == nil || response.Payload != nil {
		t.Fatalf("failed transaction returned authority: %v", err)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 1024 || b.Issued != 0 {
		t.Fatalf("failed commit consumed funds: %+v %v", b, err)
	}
}

func TestIssuerFundedAllocationRootsAndDelegationRollbackTogether(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	// Use a new finite delegation with a different source for each root. The
	// existing fixture has no references and supplies no implicit anchor.
	d.ID = shareID(701)
	d.SourceID = shareID(702)
	d.RuntimeSourceID = shareID(703)
	d.ShareSourceID = shareID(704)
	for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
		d.ShareReferences = append(d.ShareReferences, ShareReference{ID: shareID(710 + i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: d.SignerEpoch, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: d.ValidFrom, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline})
	}
	if _, e := s.db.Exec(`CREATE TRIGGER fail_funded_install BEFORE INSERT ON issuer_delegations BEGIN SELECT RAISE(ABORT,'injected durable write failure'); END;`); e != nil {
		t.Fatal(e)
	}
	if e := s.InstallFundedHubDelegation(ctx, shareID(799), d, clock); e == nil {
		t.Fatal("injected failure acknowledged")
	}
	for _, table := range []string{"share_source_anchors", "share_source_heads", "share_references"} {
		var n int
		if e := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("partial source authority", table, n, e)
		}
	}
	if _, e := s.db.Exec(`DROP TRIGGER fail_funded_install`); e != nil {
		t.Fatal(e)
	}
	// A fresher authenticated liveness sample must survive late installation;
	// the allocation cannot restore its older, longer autonomy deadline.
	newer := clock.Latest.Add(time.Second)
	shorter := d.AutonomyDeadline.Add(-time.Minute)
	if e := s.RecordBackendHeartbeat(ctx, newer, shorter); e != nil {
		t.Fatal(e)
	}
	clock.Earliest, clock.Latest = newer, newer
	if e := s.InstallFundedHubDelegation(ctx, shareID(799), d, clock); e != nil {
		t.Fatal(e)
	}
	status, e := s.CurrentAutonomyStatus(ctx, newer, true)
	if e != nil || !status.BackendHeartbeat.Equal(newer) || !status.HardDeadline.Equal(shorter) {
		t.Fatal("allocation rolled back liveness/deadline", e, status)
	}
	request := issuerRequest(d, 705)
	first, e := s.IssueHubAuthorization(ctx, request, clock)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InstallFundedHubDelegation(ctx, shareID(799), d, clock); e != nil {
		t.Fatal(e)
	}
	replay, e := s.IssueHubAuthorization(ctx, request, clock)
	if e != nil || !bytes.Equal(first.Payload, replay.Payload) {
		t.Fatal("replay changed funded issue", e)
	}
	b, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Remaining != d.RawBytes-request.RawBytes {
		t.Fatal("replayed installation refilled", e, b)
	}
}
