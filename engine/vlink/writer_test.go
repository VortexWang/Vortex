package vlink

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"vortex/engine/accounting"
)

// Actual mux/UDP execution with an ambiguous target write must retain the
// reservation, even after link and session cleanup.
func TestUDPPartialTargetWriteRetainsUnknownReservation(t *testing.T) {
	left, right := net.Pipe()
	client, err := newMuxLink(left, MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMuxLink(right, MuxConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(io.EOF)
	defer server.Close(io.EOF)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sent, err := client.Open(ctx, OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{127, 0, 0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	received, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan accounting.Outcome, 4)
	conn := &ambiguousUDPConn{done: make(chan struct{})}
	resource := &UDPResource{Conn: conn, Policy: recordingUDPSession{outcomes: outcomes}}
	if err = server.AuthorizeRemote(ctx, received.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return resource, nil }); err != nil {
		t.Fatal(err)
	}
	if err = sent.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- resource.Run(ctx, received, nil) }()
	if ok, err := sent.WriteDgram(ctx, []byte("unknown-write")); !ok || err != nil {
		t.Fatalf("accepted=%v err=%v", ok, err)
	}
	select {
	case outcome := <-outcomes:
		if outcome.Resolution != accounting.Unknown || outcome.Reserved != 13 || outcome.Used != 0 {
			t.Fatalf("released ambiguous reservation: %+v", outcome)
		}
	case <-ctx.Done():
		t.Fatal("missing unknown outcome")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("ambiguous write succeeded")
		}
	case <-ctx.Done():
		t.Fatal("UDP worker leaked")
	}
	select {
	case outcome := <-outcomes:
		t.Fatalf("cleanup resolved reservation again: %+v", outcome)
	default:
	}
}

type ambiguousUDPConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func TestUDPReceiveQueueBoundsBytesAndReleasesOnRead(t *testing.T) {
	stream := &MuxStream{link: &MuxLink{config: MuxConfig{UDPQueueBytes: 4}, done: make(chan struct{})}, receive: make(chan Frame, 2), done: make(chan struct{})}
	frame := Frame{Type: FrameDgram, Payload: []byte("abc")}
	if err := stream.enqueue(frame); err != nil {
		t.Fatal(err)
	}
	if err := stream.enqueue(frame); err == nil {
		t.Fatal("byte bound exceeded despite free frame slots")
	}
	if _, err := stream.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stream.enqueue(frame); err != nil {
		t.Fatal("dequeued bytes retained capacity:", err)
	}
	if _, err := stream.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stream.udpQueued.Load() != 0 {
		t.Fatal("receive-byte accounting leaked")
	}
}

func (c *ambiguousUDPConn) Read([]byte) (int, error)    { <-c.done; return 0, net.ErrClosed }
func (c *ambiguousUDPConn) Write(p []byte) (int, error) { return len(p) / 2, io.ErrUnexpectedEOF }
func (c *ambiguousUDPConn) Close() error                { c.once.Do(func() { close(c.done) }); return nil }
