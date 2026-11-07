package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	controlv1 "vortex/contracts/protobuf/v1"
)

// The stream fixture isolates capability selection/correlation. Actual TLS
// verification and Agent sockets are exercised by the service integration test.
type ruleStream struct {
	ctx context.Context
	in  chan *controlv1.NodeMessage
	out chan *controlv1.ControlMessage
}

func (s *ruleStream) Context() context.Context     { return s.ctx }
func (s *ruleStream) SetHeader(metadata.MD) error  { return nil }
func (s *ruleStream) SendHeader(metadata.MD) error { return nil }
func (s *ruleStream) SetTrailer(metadata.MD)       {}
func (s *ruleStream) SendMsg(any) error            { return nil }
func (s *ruleStream) RecvMsg(any) error            { return nil }
func (s *ruleStream) Send(m *controlv1.ControlMessage) error {
	select {
	case s.out <- m:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func (s *ruleStream) Recv() (*controlv1.NodeMessage, error) {
	select {
	case m := <-s.in:
		if m == nil {
			return nil, io.EOF
		}
		return m, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func TestRuleCapabilityLegacyAndReceiptIsolation(t *testing.T) {
	const node = "00000000-0000-4000-8000-000000000001"
	const boot = "00000000-0000-4000-8000-000000000002"
	for _, name := range []string{"legacy", "unknown-capability", "two-phase"} {
		t.Run(name, func(t *testing.T) {
			capable := name == "two-phase"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cert := &x509.Certificate{DNSNames: []string{node}, Subject: pkix.Name{CommonName: node}}
			ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{Version: tls.VersionTLS13, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
			s := &ruleStream{ctx: ctx, in: make(chan *controlv1.NodeMessage, 4), out: make(chan *controlv1.ControlMessage, 1)}
			connected := make(chan struct{})
			a := &Adapter{Observe: func(context.Context, string, string, []byte) error { close(connected); return nil }}
			caps := []string{}
			if capable {
				caps = append(caps, "rule-two-phase-v1")
			} else if name == "unknown-capability" {
				caps = append(caps, "rule-two-phase-v2")
			}
			s.in <- &controlv1.NodeMessage{NodeId: node, BootId: boot, Sequence: 1, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{Capabilities: caps}}}
			done := make(chan error, 1)
			go func() { done <- a.Connect(s) }()
			defer func() { cancel(); <-done }()
			select {
			case <-connected:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			command := &controlv1.RuleCommand{Phase: "prepare", TaskKey: "task", IdempotencyKey: "idem", TargetRevision: 1, FencingGeneration: 2, TransactionId: []byte("transaction-0001"), ArtifactSha256: make([]byte, 32)}
			result := make(chan ruleOutcome, 1)
			go func() { r, e := a.DeliverRule(ctx, node, boot, command); result <- ruleOutcome{receipt: r, err: e} }()
			if !capable {
				select {
				case r := <-result:
					if r.err == nil {
						t.Fatal("legacy peer accepted phased command")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case <-s.out:
					t.Fatal("RuleCommand reached legacy peer")
				default:
				}
				return
			}
			select {
			case m := <-s.out:
				if m.GetRuleCommand() != command {
					t.Fatal("wrong control body")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			newReceipt := func(revision uint64, seq uint64) *controlv1.NodeMessage {
				return &controlv1.NodeMessage{NodeId: node, BootId: boot, Sequence: seq, Body: &controlv1.NodeMessage_RuleReceipt{RuleReceipt: &controlv1.RuleReceipt{Phase: command.Phase, TaskKey: command.TaskKey, IdempotencyKey: command.IdempotencyKey, TargetRevision: revision, FencingGeneration: command.FencingGeneration, TransactionId: command.TransactionId, ArtifactSha256: command.ArtifactSha256, State: "prepared"}}}
			}
			for i, mutate := range []func(*controlv1.RuleReceipt){
				func(r *controlv1.RuleReceipt) { r.TargetRevision++ },
				func(r *controlv1.RuleReceipt) { r.Phase = "commit" },
				func(r *controlv1.RuleReceipt) { r.TaskKey = "old-task" },
				func(r *controlv1.RuleReceipt) { r.IdempotencyKey = "old-idempotency" },
				func(r *controlv1.RuleReceipt) { r.TransactionId = []byte("transaction-0002") },
				func(r *controlv1.RuleReceipt) { r.ArtifactSha256 = []byte("wrong-digest") },
				func(r *controlv1.RuleReceipt) { r.FencingGeneration++ },
			} {
				stale := newReceipt(1, uint64(i+2))
				mutate(stale.GetRuleReceipt())
				s.in <- stale
			}
			matched := newReceipt(1, 9)
			s.in <- matched
			select {
			case r := <-result:
				if r.err != nil || r.receipt != matched.GetRuleReceipt() {
					t.Fatal("uncorrelated receipt advanced task", r.err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
