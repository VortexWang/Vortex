package vlink

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestReverseConnectorRetriesThenReopensOnlyApprovedTCP(t *testing.T) {
	var attempts atomic.Int32
	var declared atomic.Int32
	var remote *MuxLink
	connector, err := NewReverseConnector(func(context.Context) (*MuxLink, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("ingress unavailable")
		}
		left, right := net.Pipe()
		localLink, err := newMuxLink(left, MuxConfig{LocalClient: true})
		if err != nil {
			return nil, err
		}
		remote, err = newMuxLink(right, MuxConfig{LocalClient: false})
		return localLink, err
	}, func(context.Context, *MuxLink) error {
		declared.Add(1)
		return nil
	}, Backoff{Initial: time.Millisecond, Maximum: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runDone := make(chan error, 1)
	go func() { runDone <- connector.Run(ctx) }()
	readyCtx, readyCancel := context.WithTimeout(context.Background(), time.Second)
	defer readyCancel()
	link, err := connector.WaitReady(readyCtx)
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() < 2 || declared.Load() != 1 || link == nil {
		t.Fatalf("attempts=%d declarations=%d link=%v", attempts.Load(), declared.Load(), link)
	}

	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{192, 0, 2, 1}}
	if _, err := connector.ReopenTCP(readyCtx, request, func(OpenRequest) bool { return false }); err == nil {
		t.Fatal("reopened TCP without idempotency approval")
	}
	stream, err := connector.ReopenTCP(readyCtx, request, func(OpenRequest) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	remoteStream, err := remote.Accept(readyCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.AuthorizeRemote(readyCtx, remoteStream.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if frame, err := stream.Receive(readyCtx); err != nil || frame.Type != FrameOpenOK {
		t.Fatalf("reopened OPEN response=%+v err=%v", frame, err)
	}
	udpRequest := request
	udpRequest.Network = 2
	if _, err := connector.ReopenTCP(readyCtx, udpRequest, func(OpenRequest) bool { return true }); err == nil {
		t.Fatal("replayed UDP after link recovery")
	}

	remote.Close(ErrProtocol)
	select {
	case <-link.Closed():
	case <-time.After(time.Second):
		t.Fatal("reverse connector retained the lost link")
	}
	connector.Stop()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("reverse connector did not stop")
	}
}

func TestReverseConnectorStopsDuringBackoff(t *testing.T) {
	connector, err := NewReverseConnector(func(context.Context) (*MuxLink, error) {
		return nil, errors.New("unreachable")
	}, func(context.Context, *MuxLink) error { return nil }, Backoff{Initial: time.Hour, Maximum: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- connector.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connector did not honor cancellation during backoff")
	}
}
