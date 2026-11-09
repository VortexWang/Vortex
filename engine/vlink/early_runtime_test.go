package vlink

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestBatchedEarlyDataWaitsAtBoundAndRetainsFINOrder(t *testing.T) {
	left, right := net.Pipe()
	client, err := newMuxLink(left, MuxConfig{LocalClient: true, MaxPendingOpenBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMuxLink(right, MuxConfig{MaxPendingOpenBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(io.EOF)
	defer server.Close(io.EOF)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.Open(ctx, OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.WriteDataFrames(ctx, []Frame{{Type: FrameData, Payload: []byte("1234")}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- stream.WriteDataFrames(ctx, []Frame{{Type: FrameData, Payload: []byte("5")}}) }()
	select {
	case err := <-done:
		t.Fatalf("unbounded early DATA: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if state, ok := client.dispatch.streams.Snapshot(stream.ID); !ok || state.LocalOpenBytes != 4 {
		t.Fatalf("early bound not retained: %+v", state)
	}
	if err := server.AuthorizeRemote(ctx, incoming.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := stream.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1234", "5"} {
		frame, err := incoming.Receive(ctx)
		if err != nil || string(frame.Payload) != want {
			t.Fatalf("DATA order: %+v %v", frame, err)
		}
		frame.Release()
	}
	frame, err := incoming.Receive(ctx)
	if err != nil || frame.Type != FrameFIN {
		t.Fatalf("FIN order: %+v %v", frame, err)
	}
}

func TestEarlyLocalFINIsBufferedUntilAuthorization(t *testing.T) {
	table := NewStreamTable(true, 8)
	id, err := table.Open(StreamTCP)
	if err != nil {
		t.Fatal(err)
	}
	if err := table.MarkLocalData(id, 4); err != nil {
		t.Fatal(err)
	}
	if err := table.MarkLocalFIN(id); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
		t.Fatal(err)
	}
	if s, ok := table.Snapshot(id); !ok || !s.LocalFIN || s.Lifecycle != StreamLocalHalfClosed {
		t.Fatalf("lost pending FIN: %+v", s)
	}
}

// reserveEarlyData snapshots Opening before MarkLocalData takes the lock.
// OPEN_OK may win that gap; rejecting stale bookkeeping must not delete the
// now-authorized stream or turn a valid DATA write into a reset.
func TestEarlyDataBookkeepingLosesToOpenOKWithoutDroppingAuthority(t *testing.T) {
	table := NewStreamTable(true, 8)
	id, err := table.Open(StreamTCP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = table.Apply(Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
		t.Fatal(err)
	}
	if err = table.MarkLocalData(id, 4); err == nil {
		t.Fatal("stale early accounting succeeded")
	}
	if state, ok := table.Snapshot(id); !ok || !state.OpenOK || state.Lifecycle != StreamOpen {
		t.Fatalf("OPEN_OK stream removed by racing bookkeeping: %+v %t", state, ok)
	}
	if _, err = table.Apply(Frame{Type: FrameData, StreamID: id, Payload: []byte("reply")}); err != nil {
		t.Fatal(err)
	}
}
