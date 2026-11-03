package vlink

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func BenchmarkStreamOpenToFirstData(b *testing.B) {
	for range b.N {
		table := NewStreamTable(true, 16*1024)
		id, err := table.Open(StreamTCP)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = table.Apply(Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
			b.Fatal(err)
		}
		if _, err = table.Apply(Frame{Type: FrameData, StreamID: id, Payload: []byte{1}}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMuxLinkMultiplexesStreamsAndHonorsWindows(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	config := MuxConfig{LocalClient: true, StreamWindow: 3, AggregateWindow: 6, MaxWindow: 32}
	client, err := newMuxLink(clientConn, config)
	if err != nil {
		t.Fatal(err)
	}
	config.LocalClient = false
	server, err := newMuxLink(serverConn, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}}
	clientA, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	serverA, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	serverB, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AcceptRemote(ctx, serverA.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.AcceptRemote(ctx, serverB.ID); err != nil {
		t.Fatal(err)
	}
	if frame, err := clientA.Receive(ctx); err != nil || frame.Type != FrameOpenOK {
		t.Fatalf("stream A open: %+v %v", frame, err)
	}
	if frame, err := clientB.Receive(ctx); err != nil || frame.Type != FrameOpenOK {
		t.Fatalf("stream B open: %+v %v", frame, err)
	}
	if err := clientA.WriteData(ctx, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := clientB.WriteData(ctx, []byte("xyz")); err != nil {
		t.Fatal(err)
	}
	frameA, err := serverA.Receive(ctx)
	if err != nil || string(frameA.Payload) != "abc" {
		t.Fatalf("stream A data: %q %v", frameA.Payload, err)
	}
	frameB, err := serverB.Receive(ctx)
	if err != nil || string(frameB.Payload) != "xyz" {
		t.Fatalf("stream B data: %q %v", frameB.Payload, err)
	}
	blocked := make(chan error, 1)
	go func() { blocked <- clientA.WriteData(ctx, []byte("d")) }()
	select {
	case err := <-blocked:
		t.Fatalf("writer was not flow-controlled: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := serverA.Consume(ctx, 3); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if frame, err := serverA.Receive(ctx); err != nil || string(frame.Payload) != "d" {
		t.Fatalf("windowed data: %q %v", frame.Payload, err)
	}
	client.Close(nil)
	server.Close(nil)
}

func TestStreamIDsParityAndNoReuse(t *testing.T) {
	client, server := NewStreamTable(true, 1024), NewStreamTable(false, 1024)
	id, err := client.Open(StreamTCP)
	if err != nil || id != 1 {
		t.Fatalf("client id=%d err=%v", id, err)
	}
	serverID, err := server.Open(StreamTCP)
	if err != nil || serverID != 2 {
		t.Fatalf("server id=%d err=%v", serverID, err)
	}
}

func TestStreamOpeningDataBoundAndOpenError(t *testing.T) {
	table := NewStreamTable(false, 4)
	request, _ := MarshalOpen(OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	const id = uint32(1)
	if _, err := table.Apply(Frame{Type: FrameOpen, StreamID: id, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if frame, err := table.Apply(Frame{Type: FrameData, StreamID: id, Payload: []byte("1234")}); err != nil || frame.Type != 0 {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameData, StreamID: id, Payload: []byte("5")}); err == nil {
		t.Fatal("accepted excess pending OPEN bytes")
	}
	if table.Count() != 0 {
		t.Fatal("overflow violation retained stream")
	}
}

func TestStreamLocalEarlyDataAndDelayedOpenError(t *testing.T) {
	table := NewStreamTable(true, 8)
	id, err := table.Open(StreamTCP)
	if err != nil {
		t.Fatal(err)
	}
	if err := table.MarkLocalData(id, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameOpenErr, StreamID: id, Payload: make([]byte, 18)}); err != nil {
		t.Fatalf("delayed OPEN_ERR: %v", err)
	}
	if table.Count() != 0 {
		t.Fatal("delayed OPEN_ERR retained stream")
	}
	id, err = table.Open(StreamTCP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameData, StreamID: id, Payload: []byte("peer")}); err == nil {
		t.Fatal("accepted server DATA before OPEN_OK")
	}
}

func TestStreamUDPRequiresOpenOKAndViolationIsLocal(t *testing.T) {
	table := NewStreamTable(true, 16)
	id, err := table.Open(StreamUDP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameDgram, StreamID: id, Payload: []byte{1}}); err == nil {
		t.Fatal("accepted DGRAM before OPEN_OK")
	} else {
		var streamErr *StreamViolation
		if !errors.As(err, &streamErr) {
			t.Fatalf("scope=%T", err)
		}
	}
	if _, ok := table.Snapshot(id); ok {
		t.Fatal("unauthorized DGRAM did not reset its stream")
	}
	id, err = table.Open(StreamUDP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameDgram, StreamID: id, Payload: []byte{1}}); err != nil {
		t.Fatal(err)
	}
}

func TestStreamRemoteOpenAndHalfCloseIsolation(t *testing.T) {
	table := NewStreamTable(true, 8)
	request, _ := MarshalOpen(OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	const remoteID = uint32(2)
	if _, err := table.Apply(Frame{Type: FrameOpen, StreamID: remoteID, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameData, StreamID: remoteID, Payload: []byte("early")}); err != nil {
		t.Fatal(err)
	}
	if err := table.AcceptRemote(remoteID); err != nil {
		t.Fatal(err)
	}
	if pending, err := table.DrainPendingOpenFrames(remoteID); err != nil || len(pending) != 1 || string(pending[0].Payload) != "early" {
		t.Fatalf("pending bytes=%d err=%v", pending, err)
	}
	if err := table.MarkLocalFIN(remoteID); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Apply(Frame{Type: FrameFIN, StreamID: remoteID}); err != nil {
		t.Fatal(err)
	}
	if table.Count() != 0 {
		t.Fatal("half-closed stream was not removed")
	}
}

func TestStreamDuplicateIDIsLinkError(t *testing.T) {
	table := NewStreamTable(true, 8)
	request, _ := MarshalOpen(OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	for range 2 {
		if _, err := table.Apply(Frame{Type: FrameOpen, StreamID: 2, Payload: request}); err != nil {
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error=%v", err)
			}
			return
		}
	}
	t.Fatal("duplicate remote stream id accepted")
}
