package vlink

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// A stream window is measured in bytes. A slow consumer may legitimately hold
// many small DATA frames while another stream continues on the same link.
func TestTCPFragmentedWindowSurvivesSlowConsumerAndHalfClose(t *testing.T) {
	left, right := net.Pipe()
	cfg := MuxConfig{LocalClient: true, StreamWindow: 32, AggregateWindow: 128, MaxWindow: 256, StreamQueueFrames: 2, MaxPendingOpenBytes: 32}
	client, err := newMuxLink(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LocalClient = false
	server, err := newMuxLink(right, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(io.EOF)
	defer server.Close(io.EOF)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	open := func() (*MuxStream, *MuxStream) {
		t.Helper()
		sent, err := client.Open(ctx, OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			t.Fatal(err)
		}
		received, err := server.Accept(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = server.AuthorizeRemote(ctx, received.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		if err = sent.WaitOpen(ctx); err != nil {
			t.Fatal(err)
		}
		return sent, received
	}
	sent, received := open()
	const payload = "fragmented-valid-window"
	for i := range payload {
		if err = sent.WriteData(ctx, []byte{payload[i]}); err != nil {
			t.Fatalf("DATA %d: %v", i, err)
		}
	}
	if err = sent.CloseWrite(ctx); err != nil {
		t.Fatal(err)
	}
	other, otherReceived := open()
	if err = other.WriteData(ctx, []byte("other")); err != nil {
		t.Fatal(err)
	}
	frame, err := otherReceived.Receive(ctx)
	if err != nil || string(frame.Payload) != "other" {
		t.Fatalf("other stream: %q %v", frame.Payload, err)
	}
	frame.Release()
	var got []byte
	for {
		frame, err = received.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type == FrameFIN {
			frame.Release()
			break
		}
		if frame.Type != FrameData {
			t.Fatalf("unexpected frame: %v", frame.Type)
		}
		got = append(got, frame.Payload...)
		frame.Release()
	}
	if string(got) != payload {
		t.Fatalf("truncated/reordered DATA: %q", got)
	}
}
