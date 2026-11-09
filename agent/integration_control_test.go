package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type configurationAckBackend struct {
	controlv1.UnimplementedNodeControlServer
	ack *controlv1.ControlMessage
}

func (b *configurationAckBackend) Connect(s grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	if _, err := s.Recv(); err != nil {
		return err
	}
	return s.Send(b.ack)
}
func startConfigurationAckBackend(t *testing.T, b *configurationAckBackend, c *tls.Config) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer(grpc.Creds(credentials.NewTLS(c)))
	controlv1.RegisterNodeControlServer(g, b)
	go g.Serve(l)
	t.Cleanup(func() { g.Stop(); l.Close() })
	return l
}
func TestConfigurationOnlyActualMTLSRejectsUsageAcknowledgements(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			f := newRenewalSafetyFixture(t)
			session := f.consumeAndHold(t)
			defer session.Close()
			usage, err := f.agent.PendingUsageOutbox(f.ctx, f.node.NodeID)
			if err != nil || len(usage) != 1 {
				t.Fatal(usage, err)
			}
			liabilities, err := f.agent.PendingUnknownUsage(f.ctx, f.node.NodeID)
			if err != nil || len(liabilities) != 1 {
				t.Fatal(liabilities, err)
			}
			ack := &controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: 1, Body: &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{usage[0].BatchID}}}}
			if unknown {
				ack.Body = &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: liabilities[0].BatchID, PayloadSha256: sha256BytesControl(liabilities[0].Payload)}}
			}
			st, ct, _ := controlTLSWithHub(t, f.node.NodeID, creditCloseBackendID)
			l := startConfigurationAckBackend(t, &configurationAckBackend{ack: ack}, st)
			called := false
			c := &ControlClient{Node: f.node, TLS: ct, ControlOnly: true, SourceBackendID: creditCloseBackendID, AcknowledgeUsage: func(context.Context, []string) error { called = true; return nil }}
			err = c.Connect(f.ctx, l.Addr().String())
			if err == nil || !strings.Contains(err.Error(), "configuration-only") {
				t.Fatalf("configuration channel accepted ACK: %v", err)
			}
			after, err := f.agent.PendingUsageOutbox(f.ctx, f.node.NodeID)
			if err != nil || len(after) != 1 || !bytes.Equal(after[0].Payload, usage[0].Payload) {
				t.Fatal("usage replay suppressed", after, err)
			}
			unknownAfter, err := f.agent.PendingUnknownUsage(f.ctx, f.node.NodeID)
			if err != nil || len(unknownAfter) != 1 || !bytes.Equal(unknownAfter[0].Payload, liabilities[0].Payload) {
				t.Fatal("unknown replay suppressed", unknownAfter, err)
			}
			if called {
				t.Fatal("configuration ACK reached usage hook")
			}
			f.assertBalance(t, 4096)
		})
	}
}
func TestExperimentalDemandRejectsProductionBackendRole(t *testing.T) {
	for _, c := range []*ControlClient{{EnableCreditDemand: true, SourceBackendID: creditCloseBackendID}, {EnableCreditDemand: true, ControlOnly: true, SourceBackendID: creditCloseBackendID}} {
		c.Node = &Node{}
		c.TLS = &tls.Config{}
		if err := c.Connect(context.Background(), "127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "independent non-regional") {
			t.Fatal("experimental role accepted production backend", err)
		}
	}
}

func TestDemandLimitCountsDistinctDurableRequests(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	st, ct, _ := controlTLSWithHub(t, f.node.NodeID, f.delegation.HubID)
	issuer := &hub.Issuer{Store: f.hub, HubID: f.delegation.HubID, Clock: func() store.IssuerTime {
		now := time.Now().UTC()
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}}
	l := startTestControlHub(t, f.ctx, &hub.TestServer{Store: f.hub, DemandIssuer: issuer, DemandLimit: 2}, st)
	conn, err := grpc.DialContext(f.ctx, l.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(ct)), grpc.WithBlock())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := controlv1.NewNodeControlClient(conn).Connect(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&controlv1.NodeMessage{NodeId: f.node.NodeID, BootId: f.node.BootID, Sequence: 1, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}); err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	var original []byte
	for i, id := range []string{first, first, second} {
		if err = stream.Send(&controlv1.NodeMessage{NodeId: f.node.NodeID, BootId: f.node.BootID, Sequence: uint64(i + 2), Body: &controlv1.NodeMessage_CreditDemand{CreditDemand: &controlv1.CreditDemand{RequestId: id, DelegationId: f.delegation.ID, RawBytes: 512, TtlSeconds: 60}}}); err != nil {
			t.Fatal(err)
		}
		response, e := stream.Recv()
		if e != nil {
			t.Fatalf("request %d closed before second distinct resolution: %v", i, e)
		}
		update := response.GetAuthorizationUpdate()
		if update == nil {
			t.Fatal(response)
		}
		if i == 0 {
			original = append([]byte(nil), update.Payload...)
		}
		if i == 1 && !bytes.Equal(original, update.Payload) {
			t.Fatal("replay changed funded bytes")
		}
	}
	balance, err := f.hub.AuditHubDelegation(f.ctx, f.delegation.ID)
	if err != nil || balance.Issued != 9216 || balance.Remaining != 7168 {
		t.Fatal("duplicate request changed debit", balance, err)
	}
}
