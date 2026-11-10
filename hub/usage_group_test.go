package hub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

type groupUsageBackend struct {
	wrong bool
	calls int
}

func (b *groupUsageBackend) AcceptUsage(context.Context, store.HubUsage) ([]byte, error) {
	panic("group fell back to individual acceptance")
}
func (b *groupUsageBackend) AcceptUsageGroup(_ context.Context, batches []store.HubUsage) ([][]byte, error) {
	b.calls++
	digests := make([][]byte, len(batches))
	for i, batch := range batches {
		digests[i] = batch.Digest
	}
	if b.wrong {
		digests[len(digests)-1] = []byte("wrong")
	}
	return digests, nil
}

func TestKnownUsageGroupRequiresEveryBackendDigestBeforeReceipts(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err := s.RecordBackendHeartbeat(ctx, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	backend := &groupUsageBackend{wrong: true}
	r := &UsageRelay{Store: s, Backend: backend, Clock: func() store.IssuerTime { return store.IssuerTime{Earliest: now, Latest: now, Trusted: true} }}
	messages := []*controlv1.NodeMessage{
		{NodeId: "node", BootId: "boot", Sequence: 1, Body: &controlv1.NodeMessage_UsageBatch{UsageBatch: &controlv1.UsageBatch{BatchId: "one", BillingSnapshotId: "snapshot"}}},
		{NodeId: "node", BootId: "boot", Sequence: 2, Body: &controlv1.NodeMessage_UsageBatch{UsageBatch: &controlv1.UsageBatch{BatchId: "two", BillingSnapshotId: "snapshot"}}},
	}
	if err := r.AcceptKnownEnvelopes(ctx, messages); err == nil {
		t.Fatal("partial backend digest group acknowledged")
	}
	pending, err := s.PendingBackendUsage(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatal("partial receipts survived", pending, err)
	}
	backend.wrong = false
	if err := r.AcceptKnownEnvelopes(ctx, messages); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingBackendUsage(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}
	if err := r.AcceptKnownEnvelopes(ctx, messages); err != nil || backend.calls != 2 {
		t.Fatal("replay repeated backend acceptance", backend.calls, err)
	}
	unknown := []*controlv1.NodeMessage{{NodeId: "node", BootId: "boot", Sequence: 3, Body: &controlv1.NodeMessage_UnknownUsageBatch{UnknownUsageBatch: &controlv1.UnknownUsageBatch{BatchId: "unknown"}}}}
	if err := r.AcceptKnownEnvelopes(ctx, unknown); err == nil {
		t.Fatal("known batch accepted unknown report")
	}
}
