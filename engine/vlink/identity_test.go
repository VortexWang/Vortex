package vlink

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestFreshnessRejectsStaleGenerationAndReplay(t *testing.T) {
	state := NewPeerFreshness(16)
	peer, boot := NodeID{1}, BootID{2}
	now := time.Now()
	if err := state.Accept(peer, boot, 3, HelloNonce{1}, now); err != nil {
		t.Fatal(err)
	}
	if err := state.Accept(peer, boot, 2, HelloNonce{2}, now.Add(time.Second)); !errors.Is(err, ErrStalePeer) {
		t.Fatalf("stale generation: %v", err)
	}
	if err := state.Accept(peer, boot, 3, HelloNonce{1}, now.Add(time.Second)); !errors.Is(err, ErrHelloReplay) {
		t.Fatalf("replay: %v", err)
	}
	if err := state.Accept(peer, BootID{3}, 1, HelloNonce{3}, now.Add(time.Second)); err != nil {
		t.Fatalf("new boot: %v", err)
	}
	if err := state.Accept(peer, boot, 4, HelloNonce{4}, now.Add(2*time.Second)); !errors.Is(err, ErrStalePeer) {
		t.Fatalf("retired boot: %v", err)
	}
}

func TestFreshnessLeaseUsesConservativeIntersection(t *testing.T) {
	tracker := NewLeaseTracker()
	start := time.Now()
	grant := LeaseGrant{RequestID: [16]byte{1}, Nonce: HelloNonce{1}, ConnectionID: [16]byte{2}, BootID: BootID{3}, Generation: 4, Revision: 1, TTL: time.Minute, RequestStart: start, ResponseAt: start.Add(time.Second), VersionDeadline: start.Add(2 * time.Minute), RuntimeDeadline: start.Add(40 * time.Second), ShareDeadline: start.Add(50 * time.Second), AutonomyDeadline: start.Add(45 * time.Second)}
	deadline, err := tracker.Accept(grant)
	if err != nil {
		t.Fatal(err)
	}
	if want := start.Add(40 * time.Second); !deadline.Equal(want) {
		t.Fatalf("deadline=%v want=%v", deadline, want)
	}
	grant.RequestID, grant.Nonce, grant.Revision = [16]byte{2}, HelloNonce{2}, 2
	grant.RequestStart = start.Add(10 * time.Second)
	grant.ResponseAt = grant.RequestStart.Add(time.Second)
	grant.RuntimeDeadline = start.Add(90 * time.Second)
	grant.VersionDeadline = start.Add(90 * time.Second)
	grant.ShareDeadline = start.Add(90 * time.Second)
	grant.AutonomyDeadline = start.Add(90 * time.Second)
	second, err := tracker.Accept(grant)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Equal(start.Add(60 * time.Second)) {
		t.Fatalf("renewal deadline=%v want=%v", second, start.Add(60*time.Second))
	}
}

func TestFreshnessLeaseRejectsSlowDuplicateAndClockRollback(t *testing.T) {
	tracker := NewLeaseTracker()
	start := time.Now()
	grant := LeaseGrant{RequestID: [16]byte{1}, Nonce: HelloNonce{1}, ConnectionID: [16]byte{2}, BootID: BootID{3}, Generation: 4, Revision: 1, TTL: time.Minute, RequestStart: start, ResponseAt: start.Add(11 * time.Second)}
	if _, err := tracker.Accept(grant); !errors.Is(err, ErrLeaseRTT) {
		t.Fatalf("slow response: %v", err)
	}
	grant.ResponseAt = start.Add(time.Second)
	if _, err := tracker.Accept(grant); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Accept(grant); !errors.Is(err, ErrLeaseReplay) {
		t.Fatalf("duplicate grant: %v", err)
	}
	grant.RequestID, grant.Nonce, grant.Revision = [16]byte{2}, HelloNonce{2}, 2
	grant.RequestStart = start.Add(-time.Second)
	grant.ResponseAt = start
	if _, err := tracker.Accept(grant); !errors.Is(err, ErrLeaseClockRollback) {
		t.Fatalf("clock rollback: %v", err)
	}
}

func TestIdentityCertificatePolicyRequiresVerifiedChainAndCanonicalSAN(t *testing.T) {
	cert, _ := testTLSIdentity(t, NodeID{1})
	parsed, err := x509.ParseCertificate(cert.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	policy := PeerCertificatePolicy{Authorize: func(context.Context, *x509.Certificate, NodeID) error { return nil }}
	if err := policy.Verify(context.Background(), []*x509.Certificate{parsed}, [][]*x509.Certificate{{parsed}}); err != nil {
		t.Fatal(err)
	}
	if err := policy.Verify(context.Background(), []*x509.Certificate{parsed}, nil); err == nil {
		t.Fatal("accepted an unverified certificate chain")
	}
	revoked := PeerCertificatePolicy{Authorize: func(context.Context, *x509.Certificate, NodeID) error { return errors.New("revoked") }}
	if err := revoked.Verify(context.Background(), []*x509.Certificate{parsed}, [][]*x509.Certificate{{parsed}}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("revocation callback err=%v", err)
	}
}

func TestIdentityTLSConfigPinsTLS13AndRequiresClientCertificate(t *testing.T) {
	base := &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
	policy := PeerCertificatePolicy{Authorize: func(context.Context, *x509.Certificate, NodeID) error { return nil }}
	server, err := TLS13Config(base, policy, true)
	if err != nil {
		t.Fatal(err)
	}
	if server.MinVersion != tls.VersionTLS13 || server.MaxVersion != tls.VersionTLS13 || server.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("TLS policy not pinned: %+v", server)
	}
	if base.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS13Config mutated caller config")
	}
}

func TestDispatcherStreamFailureIsolatedAndIdentityFailureClosesLink(t *testing.T) {
	d := NewDispatcher(true, 16)
	first, _ := d.Open(StreamTCP)
	second, _ := d.Open(StreamTCP)
	if _, err := d.Apply(Frame{Type: FrameDgram, StreamID: first, Payload: []byte{1}}); err == nil {
		t.Fatal("expected stream-local UDP violation")
	}
	if d.StreamCount() != 1 {
		t.Fatalf("stream violation affected siblings: %d", d.StreamCount())
	}
	d.IdentityFailure(errors.New("certificate revoked"))
	select {
	case <-d.Closed():
	default:
		t.Fatal("identity failure did not close link")
	}
	if d.StreamCount() != 0 || d.Err() == nil {
		t.Fatalf("link cleanup count=%d err=%v", d.StreamCount(), d.Err())
	}
	if _, err := d.Apply(Frame{Type: FrameData, StreamID: second, Payload: []byte{1}}); err == nil {
		t.Fatal("closed dispatcher accepted a frame")
	}
}

func TestConcurrentDispatcherCloseIsIdempotent(t *testing.T) {
	d := NewDispatcher(true, 8)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() { defer group.Done(); d.Close(ErrProtocol) }()
	}
	group.Wait()
	select {
	case <-d.Closed():
	default:
		t.Fatal("dispatcher did not close")
	}
}

func TestLivenessPingTimeoutAndAbsoluteIdleDeadline(t *testing.T) {
	start := time.Now()
	l := NewLiveness(start)
	if got := l.Poll(start.Add(3 * time.Second)); got != LinkSendPing {
		t.Fatalf("3s action=%v", got)
	}
	if got := l.Poll(start.Add(6 * time.Second)); got != LinkPingExpired {
		t.Fatalf("6s action=%v", got)
	}
	if got := l.Poll(start.Add(9 * time.Second)); got != LinkSendPing {
		t.Fatalf("9s action=%v", got)
	}
	if got := l.Poll(start.Add(12 * time.Second)); got != LinkUnavailable {
		t.Fatalf("12s action=%v", got)
	}
	l.Received(start.Add(13 * time.Second))
	if got := l.Poll(start.Add(15 * time.Second)); got != LinkNoAction {
		t.Fatalf("traffic did not refresh liveness: %v", got)
	}
}

func TestLivenessRejectsClockRollbackFromExtendingIdle(t *testing.T) {
	start := time.Now()
	l := NewLiveness(start)
	if got := l.Poll(start.Add(-time.Second)); got != LinkNoAction {
		t.Fatalf("rollback action=%v", got)
	}
	if got := l.Poll(start.Add(12 * time.Second)); got != LinkUnavailable {
		t.Fatalf("rollback changed deadline: %v", got)
	}
}

func TestDispatcherValidFrameRefreshesLiveness(t *testing.T) {
	start := time.Now()
	current := start
	d := NewDispatcherWithClock(true, 16, func() time.Time { return current })
	id, err := d.Open(StreamTCP)
	if err != nil {
		t.Fatal(err)
	}
	current = start.Add(4 * time.Second)
	if _, err := d.Apply(Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
		t.Fatal(err)
	}
	current = start.Add(6 * time.Second)
	if got := d.PollLiveness(); got != LinkNoAction {
		t.Fatalf("valid frame did not refresh idle clock: %v", got)
	}
}

func TestMuxPollSendsNonceBoundPingAndAcceptsPong(t *testing.T) {
	left, right := net.Pipe()
	start := time.Now()
	current := start
	var clockMu sync.Mutex
	mux, err := newMuxLink(left, MuxConfig{Now: func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return current }})
	if err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() {
		frame, err := ReadFrame(right)
		if err == nil && frame.Type != FramePing {
			err = errors.New("expected PING")
		}
		if err == nil {
			err = WriteFrame(right, Frame{Type: FramePong, Payload: frame.Payload})
		}
		peerDone <- err
	}()
	clockMu.Lock()
	current = start.Add(3 * time.Second)
	clockMu.Unlock()
	if action, err := mux.Poll(context.Background()); err != nil || action != LinkSendPing {
		t.Fatalf("poll=%v err=%v", action, err)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("PONG exchange timed out")
	}
	mux.Close(nil)
	_ = right.Close()
}
