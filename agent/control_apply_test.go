package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

const validRuntimePayload = `{"version":2,"node_id":"550e8400-e29b-41d4-a716-446655440000","listeners":[],"rules":[],"revocations":[]}`

func runtimeUpdate(payload string, txByte byte, generation, expected uint64) *controlv1.RuntimeConfigUpdate {
	data := []byte(payload)
	digest := sha256.Sum256(data)
	id := make([]byte, 16)
	for i := range id {
		id[i] = txByte
	}
	return &controlv1.RuntimeConfigUpdate{Version: 2, Payload: data, TransactionId: id, FencingGeneration: generation, PayloadSha256: digest[:], ExpectedVersion: expected}
}

func TestApplyRuntimeConfigIsValidatedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := &Reconciler{Store: s, Apply: fakeApplier{}}
	update := runtimeUpdate(validRuntimePayload, 1, 1, 0)
	result := r.ApplyRuntimeConfig(ctx, "550e8400-e29b-41d4-a716-446655440000", update)
	if result.GetState() != "active" || result.GetErrorCode() != "" || result.GetVersion() != 2 || result.GetFencingGeneration() != 1 {
		t.Fatalf("apply result=%+v", result)
	}
	replay := r.ApplyRuntimeConfig(ctx, "550e8400-e29b-41d4-a716-446655440000", update)
	if replay.GetState() != "active" || hex.EncodeToString(replay.GetTransactionId()) != hex.EncodeToString(update.GetTransactionId()) {
		t.Fatalf("replay result=%+v", replay)
	}
	conflict := runtimeUpdate(`{"version":2,"node_id":"550e8400-e29b-41d4-a716-446655440000","listeners":[],"rules":[],"revocations":[],"drain_timeout_seconds":30}`, 1, 1, 0)
	conflictResult := r.ApplyRuntimeConfig(ctx, "550e8400-e29b-41d4-a716-446655440000", conflict)
	if conflictResult.GetErrorCode() != "tx_conflict" || conflictResult.GetState() != "rejected" {
		t.Fatalf("conflicting replay=%+v", conflictResult)
	}
	if applied, err := s.AgentState(ctx, "applied_version"); err != nil || applied != "2" {
		t.Fatalf("applied version=%q err=%v", applied, err)
	}
}

func TestApplyRuntimeConfigRejectsMalformedIdentityAndDigest(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := &Reconciler{Store: s, Apply: fakeApplier{}}
	nodeID := "550e8400-e29b-41d4-a716-446655440000"
	if got := r.ApplyRuntimeConfig(ctx, nodeID, runtimeUpdate("{bad", 2, 1, 0)); got.GetErrorCode() != "invalid_config" {
		t.Fatalf("malformed payload=%+v", got)
	}
	wrongNode := `{"version":2,"node_id":"550e8400-e29b-41d4-a716-446655440001","listeners":[],"rules":[],"revocations":[]}`
	if got := r.ApplyRuntimeConfig(ctx, nodeID, runtimeUpdate(wrongNode, 3, 1, 0)); got.GetErrorCode() != "identity_mismatch" {
		t.Fatalf("wrong identity=%+v", got)
	}
	badDigest := runtimeUpdate(validRuntimePayload, 4, 1, 0)
	badDigest.PayloadSha256 = make([]byte, sha256.Size)
	if got := r.ApplyRuntimeConfig(ctx, nodeID, badDigest); got.GetErrorCode() != "payload_digest_mismatch" {
		t.Fatalf("bad digest=%+v", got)
	}
	if count, err := s.ConfigResultCount(ctx); err != nil || count != 0 {
		t.Fatalf("invalid requests reached durable prepare intent: count=%d err=%v", count, err)
	}
}

type uncertainApplier struct{}

func (uncertainApplier) Prepare([16]byte, uint64) (any, error) { return struct{}{}, nil }
func (uncertainApplier) Commit([16]byte, bool) (any, error) {
	return nil, ErrActivationUnknown
}
func (uncertainApplier) Abort([16]byte) (any, error) { return struct{}{}, nil }

func TestActivationUnknownSurvivesSQLiteReopenAndReplay(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "agent.db")
	nodeID := "550e8400-e29b-41d4-a716-446655440000"
	update := runtimeUpdate(validRuntimePayload, 5, 1, 0)
	s, err := store.Open(dbPath, "agent")
	if err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Store: s, Apply: uncertainApplier{}}
	first := r.ApplyRuntimeConfig(ctx, nodeID, update)
	if first.GetState() != "activation_unknown" || first.GetErrorCode() != "activation_unknown" {
		t.Fatalf("first result=%+v", first)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(dbPath, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	second := (&Reconciler{Store: s, Apply: fakeApplier{}}).ApplyRuntimeConfig(ctx, nodeID, update)
	if second.GetState() != "activation_unknown" || second.GetErrorCode() != "activation_unknown" {
		t.Fatalf("replayed result=%+v", second)
	}
	newRequest := runtimeUpdate(validRuntimePayload, 6, 2, 0)
	blocked := (&Reconciler{Store: s, Apply: fakeApplier{}}).ApplyRuntimeConfig(ctx, nodeID, newRequest)
	if blocked.GetErrorCode() != "activation_unknown" {
		t.Fatalf("new request bypassed unknown liability: %+v", blocked)
	}
}
