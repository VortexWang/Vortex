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

func TestIdentityAdmissionRejectsNewOpenWithoutClosingExistingStreams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, b := net.Pipe()
	var denied atomic.Bool
	check := func() error {
		if denied.Load() {
			return errors.New("identity expired or revoked")
		}
		return nil
	}
	client, err := newMuxLink(a, MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(nil)
	server, err := newMuxLink(b, MuxConfig{Admit: check})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(nil)
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}}
	original, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }
	if err = server.AuthorizeRemote(ctx, remote.ID, authorize); err != nil {
		t.Fatal(err)
	}
	if err = original.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	denied.Store(true)
	rejected, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	rejectedRemote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err = server.AuthorizeRemote(ctx, rejectedRemote.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { called = true; return nil, nil }); err == nil || called {
		t.Fatal("revoked link reached business authorizer")
	}
	if err = rejected.WaitOpen(ctx); err == nil {
		t.Fatal("new OPEN accepted")
	}
	if _, err = server.Open(ctx, request); err == nil {
		t.Fatal("outgoing new OPEN bypassed current identity")
	}
	if err = original.WriteData(ctx, []byte("original-bounded-session")); err != nil {
		t.Fatal(err)
	}
	frame, err := remote.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if string(frame.Payload) != "original-bounded-session" {
		t.Fatal("existing stream damaged")
	}
	select {
	case <-server.Closed():
		t.Fatal("new OPEN denial closed established streams")
	default:
	}
}
