package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// SQLite-backed protocol backend fixture, not the Phase 6 business backend.
type durableUsageBackend struct {
	store           *store.Store
	fail, wrongHash bool
	calls           int
}

func (b *durableUsageBackend) AcceptUsage(ctx context.Context, batch store.HubUsage) ([]byte, error) {
	b.calls++
	var err error
	if batch.Kind == "unknown" {
		err = b.store.PersistUnknownUsage(ctx, batch.NodeID, batch.Payload)
	} else {
		var known controlv1.UsageBatch
		if err = proto.Unmarshal(batch.Payload, &known); err != nil {
			return nil, err
		}
		err = b.store.PersistUsageLedger(ctx, store.UsageBatch{NodeID: batch.NodeID, BootID: batch.BootID, BatchID: batch.BatchID, SnapshotID: known.BillingSnapshotId, Payload: batch.Payload})
	}
	if err != nil {
		return nil, err
	}
	if b.fail {
		return nil, fmt.Errorf("backend ACK lost after FULL commit")
	}
	digest := sha256.Sum256(batch.Payload)
	if b.wrongHash {
		digest[0] ^= 255
	}
	return digest[:], nil
}

func TestUsageOnlineBackendAckAndAutonomousRetention(t *testing.T) {
	for _, kind := range []string{"known", "unknown"} {
		for _, mode := range []string{"online-lost-ack", "autonomous"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				dir := t.TempDir()
				agentPath, hubPath, backendPath := filepath.Join(dir, "agent.db"), filepath.Join(dir, "hub.db"), filepath.Join(dir, "backend.db")
				s, err := store.Open(agentPath, "agent")
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				hs, err := store.Open(hubPath, "hub")
				if err != nil {
					t.Fatal(err)
				}
				defer hs.Close()
				bs, err := store.Open(backendPath, "hub")
				if err != nil {
					t.Fatal(err)
				}
				defer bs.Close()
				node, err := New(controlTestNodeID, s, nil)
				if err != nil {
					t.Fatal(err)
				}
				id := "handoff-" + kind
				if kind == "unknown" {
					p, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: id, OriginBootId: node.BootID, BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440024", GrantId: "550e8400-e29b-41d4-a716-446655440021", RuleId: "rule-1", Role: "source", Direction: "egress", UnknownUpperBound: 128})
					if err != nil {
						t.Fatal(err)
					}
					if err := s.PersistUnknownUsage(ctx, node.NodeID, p); err != nil {
						t.Fatal(err)
					}
				} else if err := node.PersistUsageBatch(ctx, &controlv1.UsageBatch{BatchId: id, BillingSnapshotId: "snapshot", Deltas: []*controlv1.UsageDelta{{RuleId: "rule-1", Role: "source", Direction: "egress", Bytes: 23}}}, 0); err != nil {
					t.Fatal(err)
				}
				base := time.Now().UTC()
				deadline := base.Add(time.Hour)
				if err := hs.RecordBackendHeartbeat(ctx, base, deadline); err != nil {
					t.Fatal(err)
				}
				now := base.Add(time.Second)
				if mode == "autonomous" {
					now = base.Add(store.BackendLossGrace)
				}
				backend := &durableUsageBackend{store: bs, fail: true}
				relay := &hub.UsageRelay{Store: hs, Backend: backend, Clock: func() store.IssuerTime { return store.IssuerTime{Earliest: now, Latest: now, Trusted: true} }}
				serverTLS, clientTLS, _ := controlTLS(t, node.NodeID)
				server := &hub.TestServer{Store: hs, AcceptUsage: true, UsageBatchLimit: 1, Usage: relay}
				ln := startTestControlHub(t, ctx, server, serverTLS)
				client := &ControlClient{Node: node, TLS: clientTLS}
				err = client.Connect(ctx, ln.Addr().String())
				if mode == "online-lost-ack" && err == nil {
					t.Fatal("online ACK preceded backend confirmation")
				}
				if mode == "autonomous" && err != nil {
					t.Fatal(err)
				}
				known, err := s.PendingUsageOutbox(ctx, node.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				unknown, err := s.PendingUnknownUsage(ctx, node.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				wantPending := 1
				if mode == "autonomous" {
					wantPending = 0
					if backend.calls != 0 {
						t.Fatal("autonomy called unavailable backend")
					}
				}
				if len(known)+len(unknown) != wantPending {
					t.Fatal("incorrect Agent acknowledgement boundary")
				}
				pending, err := hs.PendingBackendUsage(ctx)
				if err != nil || len(pending) != 1 {
					t.Fatalf("Hub lost unconfirmed batch: %v %v", pending, err)
				}
				// Restart Hub after local commit: neither reconnect nor reopening
				// SQLite creates a backend confirmation or resets autonomy.
				if err := hs.Close(); err != nil {
					t.Fatal(err)
				}
				hs, err = store.Open(hubPath, "hub")
				if err != nil {
					t.Fatal(err)
				}
				defer hs.Close()
				relay.Store = hs
				server.Store = hs
				backend.fail = false
				backend.wrongHash = true
				if err := relay.ReplayPending(ctx); err == nil {
					t.Fatal("wrong backend digest accepted")
				}
				backend.wrongHash = false
				if mode == "online-lost-ack" {
					if err := client.Connect(ctx, ln.Addr().String()); err != nil {
						t.Fatal(err)
					}
				} else if err := relay.ReplayPending(ctx); err != nil {
					t.Fatal(err)
				}
				before := backend.calls
				if err := relay.ReplayPending(ctx); err != nil {
					t.Fatal(err)
				}
				if backend.calls != before {
					t.Fatal("confirmed backend batch replayed")
				}
				pending, err = hs.PendingBackendUsage(ctx)
				if err != nil || len(pending) != 0 {
					t.Fatal("backend receipt not durable", err)
				}
				status, err := hs.CurrentAutonomyStatus(ctx, now, true)
				if err != nil || !status.HardDeadline.Equal(deadline) {
					t.Fatal("replay changed autonomy deadline", err)
				}
				if mode == "autonomous" && status.Mode != store.AutonomyOffline {
					t.Fatal("replay reopened authority")
				}
				if kind == "unknown" {
					for _, path := range []string{agentPath, hubPath, backendPath} {
						db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
						if err != nil {
							t.Fatal(err)
						}
						var upper, liability string
						err = db.QueryRow(`SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches`).Scan(&upper, &liability)
						db.Close()
						if err != nil || upper != "128" || liability != "unresolved" {
							t.Fatal("handoff released unknown", upper, liability, err)
						}
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				if err := hs.Close(); err != nil {
					t.Fatal(err)
				}
				if err := bs.Close(); err != nil {
					t.Fatal(err)
				}
				saveLedgerFiles(t, "handoff-"+kind+"-"+mode, map[string]string{"agent": agentPath, "hub": hubPath, "backend": backendPath})
			})
		}
	}
}

func TestControlReportsNewUsageWithoutReconnecting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS, _ := controlTLS(t, node.NodeID)
	ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AcceptUsage: true, UsageBatchLimit: 2}, serverTLS)
	done := make(chan error, 1)
	go func() {
		done <- (&ControlClient{Node: node, TLS: clientTLS, UsageInterval: 10 * time.Millisecond}).Connect(ctx, ln.Addr().String())
	}()
	// The initial send completed with an empty outbox before records arrive.
	for {
		count, err := hs.ReceiptCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := node.PersistUsageBatch(ctx, &controlv1.UsageBatch{BatchId: "late-known", BillingSnapshotId: "snapshot", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 23}}}, 0); err != nil {
		t.Fatal(err)
	}
	payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "late-unknown", OriginBootId: node.BootID, BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440024", GrantId: "550e8400-e29b-41d4-a716-446655440021", RuleId: "rule", Role: "source", Direction: "egress", UnknownUpperBound: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PersistUnknownUsage(ctx, node.NodeID, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("usage required reconnect", ctx.Err())
	}
	known, err := s.PendingUsageOutbox(ctx, node.NodeID)
	if err != nil || len(known) != 0 {
		t.Fatal("known batch unacknowledged", err)
	}
	unknown, err := s.PendingUnknownUsage(ctx, node.NodeID)
	if err != nil || len(unknown) != 0 {
		t.Fatal("unknown batch unacknowledged", err)
	}
}
