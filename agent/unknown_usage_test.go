package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	engineaccounting "vortex/engine/accounting"
	"vortex/hub"
)

type unknownFaultHub struct {
	controlv1.UnimplementedNodeControlServer
	store *store.Store
	mode  string
}

func (s *unknownFaultHub) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	message, err := stream.Recv()
	if err != nil {
		return err
	}
	if message.GetNodeId() != hello.GetNodeId() || message.GetBootId() != hello.GetBootId() || message.GetSequence() <= hello.GetSequence() {
		return fmt.Errorf("invalid fixture identity")
	}
	if s.mode == "old-peer" {
		return status.Error(codes.Unimplemented, "1.1.0 peer has no typed unknown body")
	}
	b := message.GetUnknownUsageBatch()
	payload, err := contracts.EncodeUnknownUsage(b)
	if err != nil {
		return err
	}
	if err := s.store.PersistUnknownUsage(stream.Context(), hello.GetNodeId(), payload); err != nil {
		return err
	}
	if s.mode == "lost-ack" {
		return nil
	}
	digest := sha256.Sum256(payload)
	response := &controlv1.ControlMessage{MessageId: "550e8400-e29b-41d4-a716-446655440070", Sequence: 1}
	if s.mode == "ordinary-ack" {
		response.Body = &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{b.BatchId}}}
	} else {
		digest[0] ^= 255
		response.Body = &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: b.BatchId, PayloadSha256: digest[:]}}
	}
	return stream.Send(response)
}

func startUnknownFaultHub(t *testing.T, server *unknownFaultHub, config *tls.Config) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer(grpc.Creds(credentials.NewTLS(config)))
	controlv1.RegisterNodeControlServer(g, server)
	go func() { _ = g.Serve(ln) }()
	t.Cleanup(func() { g.Stop(); ln.Close() })
	return ln
}

func TestUnknownLiabilitySurvivesBadAckLostAckAndRestart(t *testing.T) {
	for _, mode := range []string{"lost-ack", "wrong-hash", "ordinary-ack", "old-peer"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			dir := t.TempDir()
			agentDB, hubDB := filepath.Join(dir, "agent.db"), filepath.Join(dir, "hub.db")
			s, err := store.Open(agentDB, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			hs, err := store.Open(hubDB, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			node, err := New(controlTestNodeID, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			originalBoot := node.BootID
			if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
			gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024", Clock: func() (time.Time, time.Time, bool) { return now, now, true }})
			if err != nil {
				t.Fatal(err)
			}
			session, err := gate.Open(ctx, engineaccounting.Flow{Key: "unknown-flow", RuleID: "rule-1"})
			if err != nil {
				t.Fatal(err)
			}
			batch, err := session.Reserve(ctx, engineaccounting.LeftToRight, 128)
			if err != nil {
				t.Fatal(err)
			}
			if err := batch.MarkUnknown(ctx); err != nil {
				t.Fatal(err)
			}
			pending, err := s.PendingUnknownUsage(ctx, node.NodeID)
			if err != nil || len(pending) != 1 {
				t.Fatal("missing durable unknown", err)
			}
			original := pending[0]
			serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
			fault := startUnknownFaultHub(t, &unknownFaultHub{store: hs, mode: mode}, serverTLS)
			err = (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, fault.Addr().String())
			if mode != "lost-ack" && err == nil {
				t.Fatal("invalid/unsupported acknowledgement accepted")
			}
			pending, err = s.PendingUnknownUsage(ctx, node.NodeID)
			if err != nil || len(pending) != 1 || !bytes.Equal(original.Payload, pending[0].Payload) {
				t.Fatal("fault lost immutable outbox", err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = store.Open(agentDB, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			node, err = New(controlTestNodeID, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if node.BootID == originalBoot {
				t.Fatal("restart reused boot")
			}
			pending, err = s.PendingUnknownUsage(ctx, node.NodeID)
			if err != nil || len(pending) != 1 || pending[0].OriginBootID != originalBoot || !bytes.Equal(original.Payload, pending[0].Payload) {
				t.Fatal("restart changed origin or payload", err)
			}
			ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AcceptUsage: true, UsageBatchLimit: 1}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, ln.Addr().String()); err != nil {
				t.Fatal(err)
			}
			pending, err = s.PendingUnknownUsage(ctx, node.NodeID)
			if err != nil || len(pending) != 0 {
				t.Fatal("typed ACK did not suppress replay", err)
			}
			balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440021")
			if err != nil || balance.Unknown != 128 || balance.Remaining != 3968 || balance.Committed != 0 {
				t.Fatalf("ACK released or consumed unknown: %+v %v", balance, err)
			}
			if mode == "lost-ack" {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				if err := hs.Close(); err != nil {
					t.Fatal(err)
				}
				saveNamedLedger(t, "unknown", agentDB, hubDB)
				if out := os.Getenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR"); out != "" {
					metadata, _ := json.Marshal(map[string]any{"fault": "lost-ack", "origin_boot_id": originalBoot, "sender_boot_id": node.BootID, "unknown_upper_bound": 128, "acknowledged": true, "liability": "unresolved"})
					if err := os.WriteFile(filepath.Join(out, "unknown-evidence.json"), metadata, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
