package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	engineaccounting "vortex/engine/accounting"
	"vortex/engine/relay"
)

type closurePausedWriter struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
}

func (c *closurePausedWriter) Write(p []byte) (int, error) {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	<-c.release
	return c.Conn.Write(p)
}

func closureTCPPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	client, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	for _, c := range []net.Conn{client, server} {
		if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return client, server
}

// This uses the real Engine TCP relay and Agent durable gate. Pausing after
// Reserve but before the destination write demonstrates why a stop alone is
// insufficient, even with an idle socket and no observed usage yet.
func TestCreditGrantClosureRealRelayWaitsForInflightWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	const grant = "550e8400-e29b-41d4-a716-446655440021"
	const snapshot = "550e8400-e29b-41d4-a716-446655440024"
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	r := store.AuthorizationRecord{AuthorizationID: grant, ParentAuthorizationID: "parent", SourceID: "source", Kind: "credit_grant", RuleID: "rule-1", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, BootID: n.BootID, IssuedAt: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: snapshot, AmountBytes: 4096}
	if err := s.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{IssuerID: "hub", NodeID: n.NodeID, BootID: n.BootID, UpdateID: "initial", Payload: []byte("authorized fixture"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []store.AuthorizationRecord{r}, nil); err != nil {
		t.Fatal(err)
	}
	gate, err := n.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: grant, RuleID: "rule-1", RuleRevision: 1, PathGeneration: 1, SnapshotID: snapshot, Clock: func() (time.Time, time.Time, bool) { return now, now, true }})
	if err != nil {
		t.Fatal(err)
	}
	client, left := closureTCPPair(t)
	right, target := closureTCPPair(t)
	paused := &closurePausedWriter{Conn: right, entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer func() {
		select {
		case <-paused.release:
		default:
			close(paused.release)
		}
	}()
	settled := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := relay.TCPWithGate(ctx, left, paused, gate, func(relay.Direction, int64) error {
			settled <- struct{}{}
			return nil
		})
		done <- err
	}()
	first := bytes.Repeat([]byte{'x'}, 37)
	if _, err := client.Write(first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-paused.entered:
	case <-ctx.Done():
		t.Fatal("relay never reached a reserved write")
	}
	if err := s.StopCreditGrant(ctx, n.NodeID, n.BootID, "hub", grant); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, n.NodeID, n.BootID, "hub", grant); !errors.Is(err, store.ErrCreditGrantUnsettled) {
		t.Fatalf("final evidence raced past writable Engine batch: %v", err)
	}
	close(paused.release)
	received := make([]byte, len(first))
	if _, err := io.ReadFull(target, received); err != nil || !bytes.Equal(received, first) {
		t.Fatalf("original write=%q %v", received, err)
	}
	select {
	case <-settled:
	case <-ctx.Done():
		t.Fatal("original write did not settle")
	}
	final, err := s.FinalizeStoppedCreditGrant(ctx, n.NodeID, n.BootID, "hub", grant)
	if err != nil || final.Used != 37 || final.Unused != 4059 || final.BatchCount != 1 {
		t.Fatalf("final=%+v %v", final, err)
	}
	if _, err := gate.Open(ctx, engineaccounting.Flow{Key: "new-flow", RuleID: "rule-1"}); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("stopped grant opened new relay: %v", err)
	}
	if _, err := client.Write([]byte("must-not-arrive")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, store.ErrAuthorizationDenied) {
			t.Fatalf("relay did not stop on next pre-write gate: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("relay did not stop")
	}
	if rest, err := io.ReadAll(target); err != nil || len(rest) != 0 {
		t.Fatalf("post-stop bytes arrived: %q %v", rest, err)
	}
}
