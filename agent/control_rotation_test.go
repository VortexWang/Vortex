package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

type rotationTestHub struct {
	controlv1.UnimplementedNodeControlServer
	node, boot  string
	prefix      int
	first, next *controlv1.AuthorizationUpdate
}

func (s *rotationTestHub) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	if hello.GetNodeId() != s.node || hello.GetBootId() != s.boot || hello.GetHeartbeat() == nil {
		return fmt.Errorf("invalid rotation test heartbeat")
	}
	for i := 1; i <= s.prefix+1; i++ {
		update := s.first
		if i == s.prefix+1 {
			update = s.next
		}
		if err := stream.Send(&controlv1.ControlMessage{MessageId: fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", i), Sequence: uint64(i), Body: &controlv1.ControlMessage_AuthorizationUpdate{AuthorizationUpdate: update}}); err != nil {
			return err
		}
	}
	return nil
}
func startRotationHub(t *testing.T, s *rotationTestHub, config *tls.Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(config)))
	controlv1.RegisterNodeControlServer(server, s)
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { server.Stop(); _ = ln.Close() })
	return ln.Addr().String()
}
func TestControlRotationDoesNotApplyOverLimitGrantAndReplaysOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	node, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	// These two valid immutable updates share source lineage; only their grant
	// and update identities differ. Repeating envelopes never refills a grant.
	now := time.Now().UTC()
	makeUpdate := func(suffix string) *controlv1.AuthorizationUpdate {
		var body map[string]any
		if err := json.Unmarshal(authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4), &body); err != nil {
			t.Fatal(err)
		}
		body["update_id"] = "550e8400-e29b-41d4-a716-4466554400" + suffix
		body["issued_at"] = now.Format(time.RFC3339Nano)
		body["backend_heartbeat_at"] = now.Format(time.RFC3339Nano)
		body["autonomy_deadline"] = now.Add(time.Hour).Format(time.RFC3339Nano)
		grant := body["authorizations"].([]any)[0].(map[string]any)
		grant["authorization_id"] = "550e8400-e29b-41d4-a716-4466554401" + suffix
		grant["issued_at"], grant["not_before"] = now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
		grant["expires_at"], grant["hard_deadline"] = now.Add(5*time.Minute).Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return &controlv1.AuthorizationUpdate{Generation: 4, Payload: raw}
	}
	first, next := makeUpdate("21"), makeUpdate("22")
	serverTLS, clientTLS, _ := controlTLS(t, node.NodeID)
	address := startRotationHub(t, &rotationTestHub{node: node.NodeID, boot: node.BootID, prefix: maxControlMessageIDs, first: first, next: next}, serverTLS)
	client := &ControlClient{Node: node, TLS: clientTLS}
	if err := client.Connect(ctx, address); err == nil || !strings.Contains(err.Error(), "identity limit reached") {
		t.Fatalf("expected bounded rotation: %v", err)
	}
	audit := func(want uint64) {
		t.Helper()
		b, err := db.AuditAuthorizationSource(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440023")
		if err != nil || b.Initial != want || b.Remaining != want || b.Reserved != 0 || b.Committed != 0 || b.Unknown != 0 {
			t.Fatalf("unexpected balance after rotation: %+v %v", b, err)
		}
	}
	audit(4096) // The 4097th response was not applied.
	replay := startRotationHub(t, &rotationTestHub{node: node.NodeID, boot: node.BootID, next: next}, serverTLS)
	for i := 0; i < 2; i++ {
		if err := client.Connect(ctx, replay); err != nil {
			t.Fatal(err)
		}
		audit(8192) // First retry installs it; second is an exact durable replay.
	}
}
