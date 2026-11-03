package vlink

import (
	"context"
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
