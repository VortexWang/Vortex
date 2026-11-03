package vlink

import (
	"errors"
	"testing"
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
