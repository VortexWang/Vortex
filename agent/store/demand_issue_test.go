package store

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
)

func TestDefinitiveDenialSurvivesRestartAndCannotBecomeIssuance(t *testing.T) {
	s, d, now, path := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 201)
	r.RawBytes = 2048
	out, err := s.ResolveHubDemand(ctx, r, now)
	if err != nil || out.Denial != "insufficient_funds" || len(out.Authorization.Payload) != 0 {
		t.Fatalf("denial %+v %v", out, err)
	}
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replay, err := reopened.ResolveHubDemand(ctx, r, now)
	if err != nil || replay.Denial != out.Denial {
		t.Fatalf("lost denial replay %+v %v", replay, err)
	}
	if _, err := reopened.IssueHubAuthorization(ctx, r, now); !errors.Is(err, ErrDemandDefinitivelyDenied) {
		t.Fatalf("legacy issue bypassed definitive denial: %v", err)
	}
	changed := r
	changed.RawBytes = 512
	if _, err := reopened.ResolveHubDemand(ctx, changed, now); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("changed rejected request reused: %v", err)
	}
	changed.ID = issuerRequest(d, 202).ID
	accepted, err := reopened.ResolveHubDemand(ctx, changed, now)
	if err != nil || accepted.Denial != "" || len(accepted.Authorization.Payload) == 0 {
		t.Fatalf("fresh smaller request %+v %v", accepted, err)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 512 || b.Remaining != 512 {
		t.Fatalf("denial touched funding: %+v %v", b, err)
	}
}

func TestDemandDenialWriteFailureIsNotDefinitive(t *testing.T) {
	s, d, now, _ := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 203)
	r.RawBytes = 2048
	if _, err := s.db.Exec(`CREATE TRIGGER fail_denial BEFORE INSERT ON issuer_demand_denials BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	out, err := s.ResolveHubDemand(ctx, r, now)
	if err == nil || out.Denial != "" || len(out.Authorization.Payload) != 0 {
		t.Fatalf("write failure exposed outcome: %+v %v", out, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issuer_demand_denials`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial denial %d %v", n, err)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 0 || b.Remaining != 1024 {
		t.Fatalf("failure consumed funds %+v %v", b, err)
	}
}

func TestDemandConcurrentSuccessHasOneImmutableOutcome(t *testing.T) {
	s, d, now, _ := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 204)
	var wg sync.WaitGroup
	out := make(chan DemandOutcome, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.ResolveHubDemand(ctx, r, now)
			if e != nil {
				t.Error(e)
				return
			}
			out <- v
		}()
	}
	wg.Wait()
	close(out)
	var payload []byte
	for v := range out {
		if v.Denial != "" {
			t.Fatalf("success changed to denial: %+v", v)
		}
		if payload == nil {
			payload = v.Authorization.Payload
		} else if !bytes.Equal(payload, v.Authorization.Payload) {
			t.Fatal("duplicate issued different authority")
		}
	}
	if len(payload) == 0 {
		t.Fatal("no successful response")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issuer_demand_denials`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("both success and denial exist: %d %v", n, err)
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 256 || b.Remaining != 768 {
		t.Fatalf("concurrent funding %+v %v", b, err)
	}
}
