package scheduler

import (
	"context"
	"testing"
)

func TestUDPQueueBoundedAndTCPProgress(t *testing.T) {
	s := New(4)
	if ok, r := s.Enqueue(Item{Kind: UDP, Payload: []byte("12345")}); ok || r != DropQueueFull {
		t.Fatalf("overflow ok=%v reason=%s", ok, r)
	}
	s.Enqueue(Item{Kind: TCP, Payload: []byte("tcp")})
	s.Enqueue(Item{Kind: UDP, Payload: []byte("u")})
	first, _ := s.Next(context.Background())
	if first.Kind != UDP {
		t.Fatalf("udp was starved: %v", first.Kind)
	}
	second, _ := s.Next(context.Background())
	if second.Kind != TCP {
		t.Fatalf("tcp did not progress: %v", second.Kind)
	}
}
func TestCloseUnblocksNext(t *testing.T) {
	s := New(1)
	done := make(chan error)
	go func() { _, e := s.Next(context.Background()); done <- e }()
	s.Close()
	if e := <-done; e != ErrClosed {
		t.Fatalf("err=%v", e)
	}
}
