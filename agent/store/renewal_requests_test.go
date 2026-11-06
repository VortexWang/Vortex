package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func renewalFixture(t *testing.T) (*Store, *controlv1.RenewalRequest, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := &controlv1.RenewalRequest{RequestId: "550e8400-e29b-41d4-a716-446655440001", NodeId: "550e8400-e29b-41d4-a716-446655440002", BootId: "550e8400-e29b-41d4-a716-446655440003", DelegationId: "550e8400-e29b-41d4-a716-446655440004", SourceId: "550e8400-e29b-41d4-a716-446655440005", BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440006", RuleId: "rule", RuleRevision: 1, PathGeneration: 2, FencingGeneration: 3, SignerEpoch: 4, RequestedRawBytes: 128, RequestedTtlSeconds: 60}
	r.RequestSha256, err = contracts.RenewalDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	return s, r, "550e8400-e29b-41d4-a716-446655440007", path
}

func TestRenewalJournalReopensAndRejectsChangedIntent(t *testing.T) {
	s, r, issuer, path := renewalFixture(t)
	ctx := context.Background()
	for range 2 {
		if err := s.PersistRenewalIntent(ctx, issuer, r); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	intent, err := reopened.RenewalIntent(ctx, r.RequestId)
	if err != nil || intent.State != "pending" || !proto.Equal(intent.Request, r) {
		t.Fatalf("reopen %+v %v", intent, err)
	}
	changed := proto.Clone(r).(*controlv1.RenewalRequest)
	changed.RequestedRawBytes++
	changed.RequestSha256, _ = contracts.RenewalDigest(changed)
	if err := reopened.PersistRenewalIntent(ctx, issuer, changed); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("changed intent: %v", err)
	}
	if err := reopened.PersistRenewalIntent(ctx, r.NodeId, r); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("changed issuer: %v", err)
	}
	changed = proto.Clone(r).(*controlv1.RenewalRequest)
	changed.RequestId = r.NodeId
	changed.BootId = issuer // new boot must not clear original unresolved lineage
	changed.RequestSha256, _ = contracts.RenewalDigest(changed)
	if err := reopened.PersistRenewalIntent(ctx, issuer, changed); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("second unresolved lineage: %v", err)
	}
	for _, scope := range []struct {
		issuer, boot string
		count        int
	}{{issuer, r.BootId, 1}, {issuer, issuer, 0}, {r.NodeId, r.BootId, 0}} {
		page, err := reopened.PendingRenewalIntents(ctx, scope.issuer, r.NodeId, scope.boot, "")
		if err != nil || len(page) != scope.count {
			t.Fatalf("replay scope: %+v %v", page, err)
		}
	}
}

func TestRenewalJournalInstallationRequiresExactCommittedUpdate(t *testing.T) {
	s, r, issuer, _ := renewalFixture(t)
	ctx := context.Background()
	if err := s.PersistRenewalIntent(ctx, issuer, r); err != nil {
		t.Fatal(err)
	}
	// This test invokes the trusted Store adapter directly. Full wire-schema
	// validation and actual Engine top-up are separate runtime acceptance gates.
	payload, err := json.Marshal(map[string]any{"schema_version": "1.2.0", "renewal_request_id": r.RequestId, "renewal_request_sha256": hex.EncodeToString(r.RequestSha256)})
	if err != nil {
		t.Fatal(err)
	}
	result := &controlv1.RenewalResult{RequestId: r.RequestId, RequestSha256: r.RequestSha256, Code: "ok", AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: r.PathGeneration, Payload: payload}}
	if err := s.PersistRenewalResult(ctx, issuer, result); err != nil {
		t.Fatal(err)
	}
	u := AuthorizationUpdate{IssuerID: issuer, NodeID: r.NodeId, BootID: r.BootId, UpdateID: r.RequestId, Payload: payload, SignerEpoch: r.SignerEpoch, PathGeneration: r.PathGeneration, FencingGenerationFloor: r.FencingGeneration}
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRenewalInstalled(ctx, issuer, r.NodeId, issuer, r.RequestId, u.UpdateID); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("new boot installed old intent: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_installed BEFORE UPDATE ON renewal_requests BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRenewalInstalled(ctx, issuer, r.NodeId, r.BootId, r.RequestId, u.UpdateID); err == nil {
		t.Fatal("journal completion survived injected disk failure")
	}
	i, err := s.RenewalIntent(ctx, r.RequestId)
	if err != nil || i.State != "received" {
		t.Fatalf("failed completion: %+v %v", i, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_installed`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.MarkRenewalInstalled(ctx, issuer, r.NodeId, r.BootId, r.RequestId, u.UpdateID); err != nil {
			t.Fatal(err)
		}
	}
	i, err = s.RenewalIntent(ctx, r.RequestId)
	if err != nil || i.State != "installed" {
		t.Fatalf("completion: %+v %v", i, err)
	}
	page, err := s.PendingRenewalIntents(ctx, issuer, r.NodeId, r.BootId, "")
	if err != nil || len(page) != 0 {
		t.Fatalf("completed intent replayed: %v %v", page, err)
	}
}

func TestRenewalJournalReceiptIsNotInstallation(t *testing.T) {
	s, r, issuer, _ := renewalFixture(t)
	ctx := context.Background()
	if err := s.PersistRenewalIntent(ctx, issuer, r); err != nil {
		t.Fatal(err)
	}
	result := &controlv1.RenewalResult{RequestId: r.RequestId, RequestSha256: r.RequestSha256, Code: "temporarily_unavailable", Retryable: true}
	if err := s.PersistRenewalResult(ctx, issuer, result); err != nil {
		t.Fatal(err)
	}
	intent, err := s.RenewalIntent(ctx, r.RequestId)
	if err != nil || intent.State != "pending" {
		t.Fatalf("transient: %+v %v", intent, err)
	}
	result.Code = "ok"
	result.Retryable = false
	result.AuthorizationUpdate = &controlv1.AuthorizationUpdate{Generation: r.PathGeneration, Payload: []byte(`{"not":"validated authority"}`)}
	for range 2 {
		if err := s.PersistRenewalResult(ctx, issuer, result); err != nil {
			t.Fatal(err)
		}
	}
	intent, err = s.RenewalIntent(ctx, r.RequestId)
	if err != nil || intent.State != "received" {
		t.Fatalf("receipt: %+v %v", intent, err)
	}
	if err := s.MarkRenewalInstalled(ctx, issuer, r.NodeId, r.BootId, r.RequestId, "absent"); err == nil {
		t.Fatal("receipt became active authority")
	}
	changed := proto.Clone(result).(*controlv1.RenewalResult)
	changed.AuthorizationUpdate.Payload = []byte("changed")
	if err := s.PersistRenewalResult(ctx, issuer, changed); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("changed response: %v", err)
	}
	if err := s.PersistRenewalResult(ctx, r.NodeId, result); !errors.Is(err, ErrRenewalConflict) {
		t.Fatalf("wrong issuer: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM authorization_records`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("journal minted authority: %d %v", count, err)
	}
}

func TestRenewalJournalDiskFailureAndConcurrentLineage(t *testing.T) {
	s, r, issuer, _ := renewalFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_renewal BEFORE INSERT ON renewal_requests BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistRenewalIntent(ctx, issuer, r); err == nil {
		t.Fatal("uncommitted intent accepted")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_renewal`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for _, id := range []string{r.RequestId, r.NodeId, r.BootId} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			request := proto.Clone(r).(*controlv1.RenewalRequest)
			request.RequestId = id
			request.RequestSha256, _ = contracts.RenewalDigest(request)
			err := s.PersistRenewalIntent(ctx, issuer, request)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if !errors.Is(err, ErrRenewalConflict) {
				t.Errorf("persist: %v", err)
			}
		}(id)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("concurrent outstanding intents: %d", success)
	}
	var id string
	if err := s.db.QueryRow(`SELECT request_id FROM renewal_requests`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	intent, err := s.RenewalIntent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	result := &controlv1.RenewalResult{RequestId: id, RequestSha256: intent.Request.RequestSha256, Code: "quota_exhausted"}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_result BEFORE UPDATE ON renewal_requests BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistRenewalResult(ctx, issuer, result); err == nil {
		t.Fatal("uncommitted response accepted")
	}
	after, err := s.RenewalIntent(ctx, id)
	if err != nil || after.State != "pending" || after.Result != nil || !bytes.Equal(after.Request.RequestSha256, intent.Request.RequestSha256) {
		t.Fatalf("write failure lost intent: %+v %v", after, err)
	}
}
