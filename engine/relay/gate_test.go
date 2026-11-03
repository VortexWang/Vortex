package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"vortex/engine/accounting"
)

var errTestShortWrite = errors.New("test short write")

type testGate struct {
	session *testPolicySession
	err     error
}

func (g *testGate) Open(context.Context, accounting.Flow) (accounting.Session, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.session, nil
}

type testPolicySession struct {
	mu         sync.Mutex
	outcomes   []recordedOutcome
	reserveErr error
	closed     bool
}

type recordedOutcome struct {
	direction accounting.Direction
	outcome   accounting.Outcome
}

func (s *testPolicySession) Reserve(_ context.Context, direction accounting.Direction, reserved int64) (*accounting.Batch, error) {
	if s.reserveErr != nil {
		return nil, s.reserveErr
	}
	return accounting.NewBatch(reserved, func(_ context.Context, outcome accounting.Outcome) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.outcomes = append(s.outcomes, recordedOutcome{direction: direction, outcome: outcome})
		return nil
	})
}

func (s *testPolicySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *testPolicySession) snapshot() ([]recordedOutcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedOutcome(nil), s.outcomes...), s.closed
}

func TestTCPWithGateReservesBeforeWriteAndSettlesAcceptedBytes(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, upstream := tcpPair(t)
	policySession := &testPolicySession{}
	gate := &testGate{session: policySession}
	done := make(chan error, 1)
	go func() {
		_, err := TCPWithGate(context.Background(), relayLeft, relayRight, gate, nil)
		done <- err
	}()

	if _, err := client.Write([]byte("charge-me")); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(upstream); err != nil || string(got) != "charge-me" {
		t.Fatalf("upstream read = %q, %v", got, err)
	}
	if _, err := upstream.Write([]byte("reply")); err != nil {
		t.Fatal(err)
	}
	if err := upstream.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(client); err != nil || string(got) != "reply" {
		t.Fatalf("client read = %q, %v", got, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gated TCP returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gated TCP did not finish")
	}

	outcomes, closed := policySession.snapshot()
	if !closed {
		t.Fatal("policy session was not closed")
	}
	var leftToRight, rightToLeft int64
	for _, record := range outcomes {
		outcome := record.outcome
		if outcome.Resolution != accounting.Committed || outcome.Reserved != outcome.Used {
			t.Fatalf("unexpected gated outcome: %#v", record)
		}
		if record.direction == accounting.LeftToRight {
			leftToRight += outcome.Used
		} else if record.direction == accounting.RightToLeft {
			rightToLeft += outcome.Used
		}
	}
	if leftToRight != int64(len("charge-me")) || rightToLeft != int64(len("reply")) {
		t.Fatalf("committed bytes = left-to-right %d, right-to-left %d, outcomes %#v", leftToRight, rightToLeft, outcomes)
	}
}

func TestTCPWithGateDenialPreventsDestinationWrite(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, upstream := tcpPair(t)
	policySession := &testPolicySession{reserveErr: accounting.ErrDenied}
	done := make(chan error, 1)
	go func() {
		_, err := TCPWithGate(context.Background(), relayLeft, relayRight, &testGate{session: policySession}, nil)
		done <- err
	}()
	if _, err := client.Write([]byte("denied")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, accounting.ErrDenied) {
			t.Fatalf("TCP error = %v, want gate denial", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TCP did not stop after gate denial")
	}
	_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 16)
	if n, err := upstream.Read(buf); n != 0 || err == nil {
		t.Fatalf("denied payload reached destination: n=%d err=%v data=%q", n, err, buf[:n])
	}
}

func TestTCPWithGateSettlesOnlyAcceptedPartOfShortWrite(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, upstream := tcpPair(t)
	policySession := &testPolicySession{}
	limited := &shortErrorConn{Conn: relayRight, remaining: 2, err: errTestShortWrite}
	done := make(chan error, 1)
	go func() {
		_, err := TCPWithGate(context.Background(), relayLeft, limited, &testGate{session: policySession}, nil)
		done <- err
	}()
	if _, err := client.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errTestShortWrite) {
			t.Fatalf("TCP error = %v, want short-write error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TCP did not stop after short write")
	}
	buf := make([]byte, 8)
	_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
	n, err := upstream.Read(buf)
	if err != nil || string(buf[:n]) != "ab" {
		t.Fatalf("destination accepted %q, %v; want ab", buf[:n], err)
	}
	outcomes, _ := policySession.snapshot()
	if len(outcomes) != 1 || outcomes[0].outcome.Resolution != accounting.Committed || outcomes[0].outcome.Reserved != 6 || outcomes[0].outcome.Used != 2 {
		t.Fatalf("short-write outcome = %#v", outcomes)
	}
}

func TestTCPWithGateReturnsReservationWhenNoBytesAreAccepted(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, _ := tcpPair(t)
	policySession := &testPolicySession{}
	failing := errorWriteConn{Conn: relayRight}
	done := make(chan error, 1)
	go func() {
		_, err := TCPWithGate(context.Background(), relayLeft, failing, &testGate{session: policySession}, nil)
		done <- err
	}()
	if _, err := client.Write([]byte("not-delivered")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TCP did not stop after destination failure")
	}
	outcomes, _ := policySession.snapshot()
	if len(outcomes) != 1 || outcomes[0].outcome.Resolution != accounting.Returned || outcomes[0].outcome.Used != 0 {
		t.Fatalf("failed-write outcome = %#v, want returned reservation", outcomes)
	}
}

type shortErrorConn struct {
	net.Conn
	remaining int
	err       error
}

type errorWriteConn struct {
	net.Conn
}

func (c errorWriteConn) Write([]byte) (int, error) {
	return 0, errTestShortWrite
}

func (c *shortErrorConn) Write(p []byte) (int, error) {
	if c.remaining == 0 {
		return 0, c.err
	}
	if len(p) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Write(p)
	c.remaining -= n
	if c.remaining == 0 && err == nil {
		return n, c.err
	}
	return n, err
}
