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

func TestHandoffPreservesIssuedRightsAndTransfersOnlyR(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	old, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 200), clock)
	if err != nil {
		t.Fatal(err)
	}
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	result, err := s.HandoffHub(ctx, h, clock)
	if err != nil {
		t.Fatal(err)
	}
	if result.SealedSequence != 1 || result.Remaining[d.ID] != 768 {
		t.Fatalf("handoff=%+v", result)
	}
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.HandoffHub(ctx, h, clock)
	if err != nil || again.Remaining[d.ID] != 768 {
		t.Fatalf("retry %+v %v", again, err)
	}
	h.ID = "550e8400-e29b-41d4-a716-446655440801"
	if _, err = reopened.HandoffHub(ctx, h, clock); !errors.Is(err, ErrSignerEpochClosed) {
		t.Fatalf("duplicate ownership: %v", err)
	}
	replay, err := reopened.IssueHubAuthorization(ctx, issuerRequest(d, 200), clock)
	if err != nil || !bytes.Equal(old.Payload, replay.Payload) {
		t.Fatalf("old rights changed: %v", err)
	}
	next, err := reopened.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		SignerEpoch    uint64 `json:"signer_epoch"`
		PathGeneration uint64 `json:"path_generation"`
	}
	if err = json.Unmarshal(next.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SignerEpoch != 2 || envelope.PathGeneration != d.PathGeneration {
		t.Fatalf("epochs %+v", envelope)
	}
	balance, err := reopened.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Issued != 512 || balance.Remaining != 512 {
		t.Fatalf("balance %+v %v", balance, err)
	}
	records, err := reopened.PendingLedger(ctx, 100)
	if err != nil || len(records) != 3 {
		t.Fatalf("journal %d %v", len(records), err)
	}
	checkpoint, err := reopened.ConfirmLedger(ctx, records[2], records[2].Digest)
	if err != nil || checkpoint != 0 {
		t.Fatalf("skip hole %d %v", checkpoint, err)
	}
	for i := 0; i < 2; i++ {
		checkpoint, err = reopened.ConfirmLedger(ctx, records[i], records[i].Digest)
		if err != nil {
			t.Fatal(err)
		}
	}
	if checkpoint != 3 {
		t.Fatalf("checkpoint %d", checkpoint)
	}
	if _, err = reopened.ConfirmLedger(ctx, records[0], make([]byte, 32)); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("hash mismatch %v", err)
	}
	// Confirmation retains the complete immutable history.
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM hub_ledger`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("history %d %v", count, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	saveRecoveryLedger(t, s, path)
}

func TestHandoffRevokesBeforeMovingAndRollsBackOnInvalidRevocation(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute), RevokeDelegations: []string{d.ID, "missing"}}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	if _, err := s.HandoffHub(ctx, h, clock); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("invalid revoke: %v", err)
	}
	// Entire transaction rolled back, including first revocation.
	before := clock
	before.Earliest = d.BackendHeartbeat.Add(time.Second)
	before.Latest = before.Earliest.Add(time.Second)
	if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 200), before); err != nil {
		t.Fatalf("partial revoke: %v", err)
	}
	h.RevokeDelegations = []string{d.ID}
	result, err := s.HandoffHub(ctx, h, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Remaining) != 0 {
		t.Fatal("revoked remainder transferred")
	}
	if _, err = s.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("revoked issuance %v", err)
	}
	balance, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Remaining != 768 {
		t.Fatalf("frozen remainder lost %+v %v", balance, err)
	}
}

func TestConcurrentHandoffHasSingleSuccessor(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, e := s.HandoffHub(ctx, h, clock); e != nil || r.Remaining[d.ID] != 1024 {
				t.Errorf("replay %+v %v", r, e)
			}
		}()
	}
	wg.Wait()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hub_transfers`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("transfers %d %v", count, err)
	}
}
