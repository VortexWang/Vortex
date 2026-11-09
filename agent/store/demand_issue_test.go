package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
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

func TestDemandOutcomesRemainImmutableAcrossSignerHandoff(t *testing.T) {
	s, d, now, path := issuerFixture(t)
	ctx := context.Background()
	denied := issuerRequest(d, 210)
	denied.RawBytes = 2048
	rejected, err := s.ResolveHubDemand(ctx, denied, now)
	if err != nil || rejected.Denial != "insufficient_funds" {
		t.Fatalf("initial refusal: %+v %v", rejected, err)
	}
	accepted := issuerRequest(d, 211)
	issued, err := s.ResolveHubDemand(ctx, accepted, now)
	if err != nil || issued.Denial != "" {
		t.Fatalf("initial success: %+v %v", issued, err)
	}
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440810", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	now.Earliest = h.Heartbeat
	now.Latest = h.Heartbeat.Add(time.Second)
	transferred, err := s.HandoffHub(ctx, h, now)
	if err != nil || transferred.Remaining[d.ID] != 768 {
		t.Fatalf("handoff returned issued/refused rights: %+v %v", transferred, err)
	}
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replay, err := reopened.ResolveHubDemand(ctx, denied, now)
	if err != nil || replay.Denial != rejected.Denial || len(replay.Authorization.Payload) != 0 {
		t.Fatalf("refusal changed after epoch: %+v %v", replay, err)
	}
	same, err := reopened.ResolveHubDemand(ctx, accepted, now)
	if err != nil || !bytes.Equal(same.Authorization.Payload, issued.Authorization.Payload) {
		t.Fatalf("old success reissued in new epoch: %+v %v", same, err)
	}
	fresh := issuerRequest(d, 212)
	next, err := reopened.ResolveHubDemand(ctx, fresh, now)
	if err != nil || next.Denial != "" {
		t.Fatalf("new epoch issue failed: %+v %v", next, err)
	}
	var envelope struct {
		SignerEpoch uint64 `json:"signer_epoch"`
	}
	if err := json.Unmarshal(next.Authorization.Payload, &envelope); err != nil || envelope.SignerEpoch != 2 {
		t.Fatalf("new request has wrong owner: %+v %v", envelope, err)
	}
	balance, err := reopened.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Issued != 512 || balance.Remaining != 512 {
		t.Fatalf("handoff/replay violated conservation: %+v %v", balance, err)
	}
	// A successful issue cannot be relabelled as an oversized rejected request.
	accepted.RawBytes = 4096
	if _, err := reopened.ResolveHubDemand(ctx, accepted, now); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("successful identity changed to denial: %v", err)
	}
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM issuer_demand_denials WHERE request_id=?`, accepted.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("conflict stored contradictory outcome: %d %v", count, err)
	}
}
