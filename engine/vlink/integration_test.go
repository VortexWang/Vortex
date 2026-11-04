package vlink

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestVLinkIntegrationUnauthorizedUDPDoesNotReachOpenOK(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	client, err := newMuxLink(left, MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMuxLink(right, MuxConfig{LocalClient: false})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ErrProtocol)
	defer server.Close(ErrProtocol)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := OpenRequest{RuleID: [16]byte{7}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{192, 0, 2, 1}}
	stream, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AuthorizeRemote(ctx, remote.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) {
		return nil, newOpenRejection(OpenUnauthorized)
	}); err == nil {
		t.Fatal("unauthorized OPEN accepted")
	}
	if frame, err := stream.Receive(ctx); err != nil || frame.Type != FrameOpenErr {
		t.Fatalf("OPEN_ERR=%+v err=%v", frame, err)
	}
	if accepted, err := stream.WriteDgram(ctx, []byte("blocked")); err == nil || accepted {
		t.Fatalf("unauthorized DGRAM accepted=%v err=%v", accepted, err)
	}
}

func TestLateWindowForClosedStreamDoesNotResetOrCloseLink(t *testing.T) {
	left, right := net.Pipe()
	client, err := newMuxLink(left, MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMuxLink(right, MuxConfig{LocalClient: false})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ErrProtocol)
	defer server.Close(ErrProtocol)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}}
	local, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client.removeStream(local.ID)
	server.removeStream(remote.ID)
	var credit [4]byte
	binary.BigEndian.PutUint32(credit[:], 1)
	if err := client.write(ctx, Frame{Type: FrameWindow, StreamID: local.ID, Payload: credit[:]}); err != nil {
		t.Fatal(err)
	}
	if err := client.write(ctx, Frame{Type: FrameRST, StreamID: local.ID, Payload: []byte{0, 8}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	select {
	case <-server.Closed():
		t.Fatalf("late stream credit closed link: %v", server.Err())
	default:
	}
	if server.dispatch.streams.Count() != 0 || client.dispatch.streams.Count() != 0 {
		t.Fatal("late WINDOW recreated a closed stream")
	}
}

func TestResetRemovesBothStreamRecords(t *testing.T) {
	left, right := net.Pipe()
	client, err := newMuxLink(left, MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMuxLink(right, MuxConfig{LocalClient: false})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ErrProtocol)
	defer server.Close(ErrProtocol)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{127, 0, 0, 1}}
	local, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AuthorizeRemote(ctx, remote.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := local.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	if err := local.Reset(ctx, 8); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && server.dispatch.streams.Count() != 0 {
		time.Sleep(time.Millisecond)
	}
	if client.dispatch.streams.Count() != 0 || server.dispatch.streams.Count() != 0 {
		t.Fatalf("stream records remain: local=%d remote=%d", client.dispatch.streams.Count(), server.dispatch.streams.Count())
	}
}
