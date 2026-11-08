package hub

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// RuntimeServer composes durable funded issuance, renewals and usage transport.
// It accepts no tenant policy, runtime configuration or unprovisioned budget
// from a Node. Rule preparation/Commit remains on the direct backend channel.
type RuntimeServer struct {
	controlv1.UnimplementedNodeControlServer
	HubID, Region string
	Issuer        *Issuer
	Usage         *UsageRelay
	VerifyPeer    func(*x509.Certificate) error
	mu            sync.Mutex
	sessions      map[string]*runtimeSession
}
type runtimeSession struct {
	mu     sync.Mutex
	active bool
	cancel context.CancelFunc
}

func (s *RuntimeServer) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	if s.Issuer == nil || s.Issuer.Store == nil || s.Issuer.Clock == nil || s.Issuer.HubID != s.HubID || !validStatusUUID(s.HubID) || s.Region == "" || s.Usage == nil || s.Usage.Store != s.Issuer.Store {
		return errors.New("Hub runtime dependencies unavailable")
	}
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	p, ok := peer.FromContext(ctx)
	if !ok {
		return errors.New("Node peer missing")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || info.State.Version != tls.VersionTLS13 || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return errors.New("verified TLS1.3 Node required")
	}
	cert := info.State.PeerCertificates[0]
	node := cert.Subject.CommonName
	if !validStatusUUID(node) || len(cert.DNSNames) != 1 || cert.DNSNames[0] != node {
		return errors.New("ambiguous Node identity")
	}
	verify := func() error {
		now := s.Issuer.Clock()
		if !validFundedClock(now) || now.Earliest.Before(cert.NotBefore) || !now.Latest.Before(cert.NotAfter) {
			return store.ErrAuthorityTimeUncertain
		}
		if s.VerifyPeer != nil {
			return s.VerifyPeer(cert)
		}
		return nil
	}
	if e := verify(); e != nil {
		return e
	}
	first, e := stream.Recv()
	if e != nil {
		return e
	}
	if first.NodeId != node || !validStatusUUID(first.BootId) || first.Sequence == 0 || first.GetHeartbeat() == nil || len(first.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("Node runtime handshake mismatch")
	}
	helloMessage, e := stream.Recv()
	if e != nil {
		return e
	}
	hello := helloMessage.GetLifecycleHello()
	if helloMessage.NodeId != node || helloMessage.BootId != first.BootId || helloMessage.Sequence <= first.Sequence || len(helloMessage.ProtoReflect().GetUnknown()) != 0 || hello == nil || hello.ControlVersion != "1.3.0" || hello.RegionId != s.Region || !validStatusUUID(hello.SessionId) || !validLifecycleCapabilities(hello.Capabilities, true) || len(hello.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("unnegotiated runtime lifecycle")
	}
	current := &runtimeSession{active: true, cancel: cancel}
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*runtimeSession{}
	}
	if old := s.sessions[node]; old != nil {
		old.mu.Lock()
		old.active = false
		old.cancel()
		old.mu.Unlock()
	}
	s.sessions[node] = current
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.sessions[node] == current {
			delete(s.sessions, node)
		}
		s.mu.Unlock()
	}()
	mutate := func(fn func() error) error {
		current.mu.Lock()
		defer current.mu.Unlock()
		if !current.active || ctx.Err() != nil {
			return errors.New("stale Node runtime session")
		}
		if e := verify(); e != nil {
			return e
		}
		return fn()
	}
	var sequence uint64
	send := func(m *controlv1.ControlMessage) error {
		sequence++
		m.MessageId, m.Sequence = uuid.NewString(), sequence
		return stream.Send(m)
	}
	status := func() (*controlv1.HubStatus, error) {
		if e := verify(); e != nil {
			return nil, e
		}
		now := s.Issuer.Clock()
		if _, e := s.Issuer.Store.CurrentAutonomyStatus(ctx, now.Latest, true); e != nil {
			return nil, e
		}
		snapshot, e := s.Issuer.Store.HubControlSnapshot(ctx, s.HubID, now.Latest)
		if e != nil {
			return nil, e
		}
		return &controlv1.HubStatus{HubId: s.HubID, RegionId: s.Region, SessionId: hello.SessionId, ControlVersion: "1.3.0", Capabilities: append([]string(nil), hello.Capabilities...), Mode: snapshot.Mode, SignerEpoch: snapshot.Epoch, BackendHeartbeatAt: snapshot.BackendHeartbeat.UTC().Format(time.RFC3339Nano), AutonomyDeadline: snapshot.Deadline.UTC().Format(time.RFC3339Nano), ConfirmedSequence: snapshot.Confirmed, PendingTailRecords: snapshot.Pending}, nil
	}
	cursor := ""
	sent := map[string]bool{}
	refresh := func() error {
		st, e := status()
		if e != nil {
			return e
		}
		if e = send(&controlv1.ControlMessage{Body: &controlv1.ControlMessage_HubStatus{HubStatus: st}}); e != nil {
			return e
		}
		if st.Mode != "online" {
			return nil
		} // autonomy renews only explicit existing requests
		var page store.BootstrapPage
		if e = mutate(func() error {
			var err error
			page, err = s.Issuer.Store.BootstrapHubAuthorizations(ctx, s.HubID, node, first.BootId, cursor, s.Issuer.Clock())
			return err
		}); e != nil {
			return e
		}
		cursor = page.After
		if page.Complete {
			cursor = ""
		}
		for _, update := range page.Updates {
			hash := sha256.Sum256(update.Payload)
			key := string(hash[:])
			if sent[key] {
				continue
			}
			if len(sent) >= 4096 {
				return errors.New("Node runtime session replay bound exhausted")
			}
			if e = send(&controlv1.ControlMessage{Body: &controlv1.ControlMessage_AuthorizationUpdate{AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: update.Generation, Payload: update.Payload}}}); e != nil {
				return e
			}
			sent[key] = true
		}
		return nil
	}
	if e = refresh(); e != nil {
		return e
	}
	type incoming struct {
		m *controlv1.NodeMessage
		e error
	}
	received := make(chan incoming, 1)
	go func() {
		for {
			m, e := stream.Recv()
			select {
			case received <- incoming{m, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	last := helloMessage.Sequence
	for {
		var next incoming
		select {
		case <-ctx.Done():
			return ctx.Err()
		case next = <-received:
		}
		if next.e != nil {
			return next.e
		}
		m := next.m
		if m.NodeId != node || m.BootId != first.BootId || m.Sequence <= last || len(m.ProtoReflect().GetUnknown()) != 0 {
			return errors.New("Node runtime envelope mismatch")
		}
		last = m.Sequence
		if e = verify(); e != nil {
			return e
		}
		switch body := m.Body.(type) {
		case *controlv1.NodeMessage_Heartbeat:
			if e = refresh(); e != nil {
				return e
			}
		case *controlv1.NodeMessage_LifecycleHello:
			if !proto.Equal(hello, body.LifecycleHello) {
				return errors.New("changed runtime negotiation")
			}
			if e = refresh(); e != nil {
				return e
			}
		case *controlv1.NodeMessage_RenewalRequest:
			if len(hello.Capabilities) != 2 {
				return errors.New("renewal capability missing")
			}
			var result *controlv1.RenewalResult
			if e = mutate(func() error {
				var err error
				result, err = s.Issuer.Renew(ctx, node, first.BootId, body.RenewalRequest)
				return err
			}); e != nil {
				return e
			}
			if e = send(&controlv1.ControlMessage{Body: &controlv1.ControlMessage_RenewalResult{RenewalResult: result}}); e != nil {
				return e
			}
		case *controlv1.NodeMessage_UsageBatch, *controlv1.NodeMessage_UnknownUsageBatch:
			if e = s.Usage.AcceptEnvelope(ctx, m); e != nil {
				return e
			}
			if b := m.GetUsageBatch(); b != nil {
				e = send(&controlv1.ControlMessage{Body: &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{b.BatchId}}}})
			} else {
				b := m.GetUnknownUsageBatch()
				raw, err := contracts.EncodeUnknownUsage(b)
				if err != nil {
					return err
				}
				digest := sha256.Sum256(raw)
				e = send(&controlv1.ControlMessage{Body: &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: b.BatchId, PayloadSha256: digest[:]}}})
			}
			if e != nil {
				return e
			}
		case *controlv1.NodeMessage_NodeReport:
			// Observations create no rights. Preserve the exact authenticated
			// envelope for later backend report processing.
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
			if err != nil {
				return err
			}
			if e = s.Issuer.Store.AppendEvent(ctx, "runtime-report/"+node+"/"+first.BootId+"/"+hello.SessionId+"/"+uuid.NewString(), "runtime-node-report", raw); e != nil {
				return e
			}
		case *controlv1.NodeMessage_Event:
			if body.Event == nil || body.Event.Kind != "boot-recovery" || !validStatusUUID(body.Event.EventId) || len(body.Event.Payload) > 1<<20 {
				return errors.New("invalid Node recovery observation")
			}
			if e = s.Issuer.Store.PersistRecoveryEvent(ctx, node, body.Event.EventId, body.Event.Payload); e != nil {
				return e
			}
		default:
			return errors.New("unsupported Node runtime operation")
		}
	}
}
