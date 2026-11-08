package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"testing"
	"time"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestShareHandoffLegacyCapabilityNeverReceivesCAS(t *testing.T) {
	const node = "00000000-0000-4000-8000-000000000001"
	const boot = "00000000-0000-4000-8000-000000000002"
	for _, caps := range [][]string{nil, {"share-cas-v2"}, {"rule-two-phase-v1"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cert := &x509.Certificate{DNSNames: []string{node}, Subject: pkix.Name{CommonName: node}}
		ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{Version: tls.VersionTLS13, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
		s := &ruleStream{ctx: ctx, in: make(chan *controlv1.NodeMessage, 1), out: make(chan *controlv1.ControlMessage, 1)}
		connected := make(chan struct{})
		a := &Adapter{Shares: &service.ShareService{}, Observe: func(context.Context, string, string, []byte) error { close(connected); return nil }}
		s.in <- &controlv1.NodeMessage{NodeId: node, BootId: boot, Sequence: 1, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{Capabilities: caps}}}
		done := make(chan error, 1)
		go func() { done <- a.Connect(s) }()
		select {
		case <-connected:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		e := a.DeliverShare(ctx, service.TenantScope{}, "task", "token", node, boot, &controlv1.ShareCommand{Phase: "cas"})
		if e == nil {
			t.Fatal("legacy accepted CAS")
		}
		select {
		case <-s.out:
			t.Fatal("share command sent without negotiation")
		default:
		}
		cancel()
		<-done
	}
}
