package hub

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"vortex/agent/identity"
)

func enrollmentSecurityFixture(t *testing.T) (*EnrollmentServer, *tls.Config) {
	t.Helper()
	now := time.Now()
	ca, der, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := identity.GenerateCSR("hub")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.SignCSR(csr, "hub", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = identity.AttachKey(cred, key, der)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewEnrollmentServer(ca, der, map[string]bool{"once": true}, &tls.Config{Certificates: []tls.Certificate{cred.Certificate}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	return server, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "hub"}
}

func startEnrollmentSecurity(t *testing.T, s *EnrollmentServer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); ln.Close() })
	return ln.Addr().String(), cancel, done
}

func TestEnrollmentRejectsOversizeWithoutConsumingToken(t *testing.T) {
	s, config := enrollmentSecurityFixture(t)
	addr, _, _ := startEnrollmentSecurity(t, s)
	_, csr, err := identity.GenerateCSR("node-1")
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"type": "register", "node_id": "node-1", "token": "once", "csr_der": csr, "padding": strings.Repeat("x", 128*1024)}
	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	// A rejected connection may close during the write, which is also correct.
	_ = json.NewEncoder(conn).Encode(request)
	var response enrollmentResponse
	_ = json.NewDecoder(conn).Decode(&response)
	conn.Close()
	if response.Type == "registered" {
		t.Fatal("oversized registration accepted and token consumed")
	}
	conn, err = tls.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err = json.NewEncoder(conn).Encode(enrollmentRequest{Type: "register", NodeID: "node-1", Token: "once", CSR: csr}); err != nil {
		t.Fatal(err)
	}
	if err = json.NewDecoder(conn).Decode(&response); err != nil || response.Type != "registered" {
		t.Fatalf("valid retry rejected: %+v %v", response, err)
	}
}

func TestEnrollmentStalledRequestTimesOut(t *testing.T) {
	s, config := enrollmentSecurityFixture(t)
	s.RequestTimeout = 150 * time.Millisecond
	addr, _, _ := startEnrollmentSecurity(t, s)
	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err = conn.Write([]byte(`{"type":"register","token":"`)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var response enrollmentResponse
	_ = json.NewDecoder(conn).Decode(&response)
	if time.Since(started) > time.Second {
		t.Fatal("server failed to bound incomplete request")
	}
}

func TestEnrollmentCancellationClosesStalledTLS(t *testing.T) {
	s, _ := enrollmentSecurityFixture(t)
	s.RequestTimeout = 3 * time.Second
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &acceptedListener{Listener: base, accepted: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	first, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	select {
	case <-ln.accepted:
	case <-time.After(time.Second):
		t.Fatal("connection not accepted")
	}
	// An incomplete TLS handshake must not hold shutdown open.
	if _, err = first.Write([]byte{22, 3, 3, 0, 5}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve retained listener or TLS handler after cancellation")
	}
	first.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = first.Read(make([]byte, 1)); err == nil {
		t.Fatal("stalled connection remained readable after cancellation")
	}
}

type acceptedListener struct {
	net.Listener
	accepted chan struct{}
}

func (l *acceptedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted <- struct{}{}
	}
	return c, err
}

func TestEnrollmentRejectsConnectionsAboveLimit(t *testing.T) {
	s, _ := enrollmentSecurityFixture(t)
	s.MaxConnections = 1
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &acceptedListener{Listener: base, accepted: make(chan struct{}, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	first, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	select {
	case <-ln.accepted:
	case <-time.After(time.Second):
		t.Fatal("first connection not accepted")
	}
	// Serve reserves the slot synchronously before the next Accept.
	second, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetReadDeadline(time.Now().Add(time.Second))
	_, err = second.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("overload connection was accepted")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("overload connection held instead of closed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler cleanup did not complete")
	}
}
