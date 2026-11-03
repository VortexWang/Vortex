package vlink

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"vortex/engine/accounting"
)

func TestTracerTCPHalfCloseCarriesResponse(t *testing.T) {
	clientID, serverID := NodeID{11}, NodeID{22}
	clientTLS, clientSigner := testTLSIdentity(t, clientID)
	serverTLS, serverSigner := testTLSIdentity(t, serverID)
	trustTLS13Peers(t, clientTLS, serverTLS)
	clientRaw, serverRaw := net.Pipe()
	clientLink := tls.Client(clientRaw, clientTLS)
	serverLink := tls.Server(serverRaw, serverTLS)
	application, tracerLocal := tcpPair(t)
	target, targetPeer := tcpPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	peerVerifier := func(context.Context, *x509.Certificate, HelloMessage) error { return nil }
	gate := &tracerTestGate{outcomes: make(chan accounting.Outcome, 2)}
	metered := make(chan accounting.Direction, 2)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeTCPWithGate(ctx, serverLink, HelloConfig{NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: peerVerifier}, func(_ context.Context, peer NodeID, request OpenRequest) (net.Conn, uint16) {
			if peer != clientID || request.RuleRevision != 9 {
				return nil, 2
			}
			return target, 0
		}, gate, func(direction accounting.Direction, _ int64) error { metered <- direction; return nil })
	}()
	clientDone := make(chan error, 1)
	go func() {
		clientDone <- TraceTCP(ctx, clientLink, tracerLocal, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{1}, Generation: 1, Signer: clientSigner, VerifyPeer: peerVerifier}, OpenRequest{RuleID: [16]byte{3}, RuleRevision: 9, PathGeneration: 4, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	}()
	go func() {
		defer targetPeer.Close()
		buf, err := io.ReadAll(targetPeer)
		if err != nil {
			t.Errorf("target read: %v", err)
			return
		}
		if string(buf) != "request" {
			t.Errorf("target got %q", buf)
		}
		_, _ = targetPeer.Write([]byte("response"))
		_ = targetPeer.CloseWrite()
	}()
	if _, err := application.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := application.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 32)
	n, err := application.Read(response)
	if err != nil {
		select {
		case traceErr := <-clientDone:
			select {
			case serverErr := <-serverDone:
				t.Fatalf("local read: %v; tracer: %v; server: %v", err, traceErr, serverErr)
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("local read: %v; tracer: %v", err, traceErr)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal(err)
		}
	}
	if string(response[:n]) != "response" {
		t.Fatalf("client got %q", response[:n])
	}
	if _, err := application.Read(response); err == nil {
		t.Fatal("expected response-side EOF")
	}
	_ = application.Close()
	cancel()
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("client tracer did not clean up")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server tracer did not clean up")
	}
	if len(gate.outcomes) != 2 || len(metered) != 2 {
		t.Fatalf("accounting outcomes=%d meter events=%d", len(gate.outcomes), len(metered))
	}
}

func TestOpenRoundTripAndBounds(t *testing.T) {
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 2, PathGeneration: 3, HopIndex: 4, Network: 1, Visited: []NodeID{{5}}, ClientAddress: []byte{10, 0, 0, 1}}
	payload, err := MarshalOpen(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseOpen(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleID != request.RuleID || got.RuleRevision != request.RuleRevision || len(got.Visited) != 1 {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if _, err := ParseOpen(payload[:len(payload)-1]); err == nil {
		t.Fatal("accepted truncated address")
	}
}

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptTCP()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	dialed, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case conn := <-accepted:
		return dialed, conn
	case err := <-acceptErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("TCP accept timed out")
	}
	return nil, nil
}

type tracerTestGate struct {
	outcomes chan accounting.Outcome
	mu       sync.Mutex
	opens    int
}

func (g *tracerTestGate) Open(context.Context, accounting.Flow) (accounting.Session, error) {
	g.mu.Lock()
	g.opens++
	g.mu.Unlock()
	return tracerTestSession{outcomes: g.outcomes}, nil
}

type tracerTestSession struct{ outcomes chan accounting.Outcome }

func (s tracerTestSession) Reserve(_ context.Context, _ accounting.Direction, amount int64) (*accounting.Batch, error) {
	return accounting.NewBatch(amount, func(_ context.Context, outcome accounting.Outcome) error { s.outcomes <- outcome; return nil })
}
func (tracerTestSession) Close() error { return nil }
