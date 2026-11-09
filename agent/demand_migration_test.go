package agent

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type migrationDenialHub struct {
	controlv1.UnimplementedNodeControlServer
	oldTag bool
}

func (s *migrationDenialHub) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	d := &controlv1.CreditDemandDenied{RequestId: "550e8400-e29b-41d4-a716-446655440011", Reason: controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_EXPIRED}
	msg := &controlv1.ControlMessage{MessageId: "550e8400-e29b-41d4-a716-446655440012", Sequence: 1, Body: &controlv1.ControlMessage_CreditDemandDenied{CreditDemandDenied: d}}
	if s.oldTag {
		body, _ := proto.Marshal(d)
		msg.Body = nil
		msg.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, 15, protowire.BytesType), body))
	}
	return stream.Send(msg)
}
func TestDemandMigrationRejectsOldTagAndDisabledPeer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		old, enabled bool
		want         string
	}{
		{"disabled-new-tag", false, false, "unnegotiated experimental demand denial"},
		{"enabled-old-tag", true, true, "unnegotiated Hub status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
			st, ct, _ := controlTLSForHub(t, node.NodeID, "550e8400-e29b-41d4-a716-446655440013")
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			server := grpc.NewServer(grpc.Creds(credentials.NewTLS(st)))
			defer server.Stop()
			controlv1.RegisterNodeControlServer(server, &migrationDenialHub{oldTag: tc.old})
			go server.Serve(ln)
			err = (&ControlClient{Node: node, TLS: ct, EnableCreditDemand: tc.enabled}).Connect(ctx, ln.Addr().String())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}

func TestDemandMigrationHubRejectsOldRequestTag(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st, ct, _ := controlTLS(t, controlTestNodeID)
	ln := startTestControlHub(t, ctx, &hub.TestServer{Store: db, AcceptUsage: true, DemandIssuer: &hub.Issuer{Store: db}}, st)
	conn, err := grpc.DialContext(ctx, ln.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(ct)), grpc.WithBlock())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := controlv1.NewNodeControlClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := &controlv1.NodeMessage{NodeId: controlTestNodeID, BootId: "550e8400-e29b-41d4-a716-446655440013", Sequence: 1, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{}}}
	if err = stream.Send(first); err != nil {
		t.Fatal(err)
	}
	body, _ := proto.Marshal(&controlv1.CreditDemand{RequestId: "550e8400-e29b-41d4-a716-446655440011", DelegationId: "550e8400-e29b-41d4-a716-446655440014", RawBytes: 1, TtlSeconds: 1})
	first.Sequence = 2
	first.Body = nil
	first.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, 16, protowire.BytesType), body))
	if err = stream.Send(first); err != nil {
		t.Fatal(err)
	}
	if response, err := stream.Recv(); err == nil || !strings.Contains(err.Error(), "changed lifecycle hello") {
		t.Fatalf("old demand was not rejected: %v %v", response, err)
	}
}
