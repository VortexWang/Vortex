package relay

import (
	"context"
	"sync"
	"testing"
	"time"
	"vortex/engine/vlink"
)

func TestDrainRejectsNewOpenOnEstablishedMux(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, server := authenticatedPair(t, ctx)
	defer client.Close(nil)
	defer server.Close(nil)
	service := &Service{}
	service.BeginDrain()
	go service.ServeLink(ctx, server)
	for _, network := range []uint8{1, 2} {
		stream, err := client.Open(ctx, vlink.OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: network, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.WaitOpen(ctx); err == nil {
			t.Fatal("drain admitted new TCP/UDP OPEN")
		}
	}
	if stats := service.Stats(); stats.ActiveSessions != 0 || stats.DrainState != "drained" {
		t.Fatalf("%+v", stats)
	}
}

func TestDrainWaitsForWorkerAndDoesNotInventCleanup(t *testing.T) {
	s := &Service{active: 1, idle: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Drain(ctx); err == nil {
		t.Fatal("unfinished worker reported drained")
	}
	if s.Stats().DrainState != "draining" {
		t.Fatal("deadline erased live work")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.BeginDrain(); _ = s.Stats() }()
	}
	wg.Wait()
	s.mu.Lock()
	s.active = 0
	close(s.idle)
	s.mu.Unlock()
	if err := s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
