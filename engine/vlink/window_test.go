package vlink

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowAcquiresStreamAndAggregateCredit(t *testing.T) {
	w := NewWindowManager(10, 20)
	if err := w.AddStream(1, 8); err != nil {
		t.Fatal(err)
	}
	if err := w.AddStream(3, 8); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 6); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 3, 4); err != nil {
		t.Fatal(err)
	}
	if got := w.Snapshot(); got.AggregateAvailable != 0 || got.StreamAvailable[1] != 2 {
		t.Fatalf("window snapshot: %+v", got)
	}
}

func TestUDPWindowRejectionAttributesCreditAtAdmission(t *testing.T) {
	w := NewWindowManager(8, 16)
	if err := w.AddStream(1, 8); err != nil {
		t.Fatal(err)
	}
	if err := w.AddStream(3, 8); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 8); err != nil {
		t.Fatal(err)
	}
	if ok, why, err := w.tryAcquire(3, 1); err != nil || ok || why.stream || !why.aggregate {
		t.Fatalf("aggregate-only rejection: accepted=%v why=%+v err=%v", ok, why, err)
	}
	if err := w.Grant(0, 8); err != nil {
		t.Fatal(err)
	}
	if ok, why, err := w.tryAcquire(1, 1); err != nil || ok || !why.stream || why.aggregate {
		t.Fatalf("stream-only rejection: accepted=%v why=%+v err=%v", ok, why, err)
	}
	if ok, why, err := w.tryAcquire(3, 1); err != nil || !ok || why.stream || why.aggregate {
		t.Fatalf("available credit: accepted=%v why=%+v err=%v", ok, why, err)
	}
}

func TestTCPAcquirePreservesAggregateUDPReserve(t *testing.T) {
	w := NewWindowManagerWithReserve(8, 16, 2)
	if err := w.AddStream(1, 8); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 6); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := w.Acquire(ctx, 1, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("TCP consumed reserved aggregate credit: %v", err)
	}
	if ok, err := w.TryAcquire(1, 2); err != nil || !ok {
		t.Fatalf("reserved credit unavailable to UDP admission: ok=%v err=%v", ok, err)
	}
}

func TestWindowBlockedWriterWakesOnGrant(t *testing.T) {
	w := NewWindowManager(4, 16)
	if err := w.AddStream(1, 4); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 4); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- w.Acquire(context.Background(), 1, 3) }()
	select {
	case err := <-result:
		t.Fatalf("writer did not block: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := w.Grant(0, 4); err != nil {
		t.Fatal(err)
	}
	if err := w.Grant(1, 4); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer was not woken")
	}
}

func TestWindowCancellationCloseOverflowAndUDPDrop(t *testing.T) {
	w := NewWindowManager(2, 8)
	if err := w.AddStream(1, 2); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := w.Acquire(ctx, 1, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel err=%v", err)
	}
	if ok, err := w.TryAcquire(1, 1); err != nil || ok {
		t.Fatalf("UDP over-window: ok=%v err=%v", ok, err)
	}
	if err := w.Grant(1, 9); err == nil {
		t.Fatal("accepted overflow grant")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- w.Acquire(context.Background(), 1, 1) }()
	time.Sleep(10 * time.Millisecond)
	w.Close()
	select {
	case err := <-closeResult:
		if !errors.Is(err, ErrWindowClosed) {
			t.Fatalf("close err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not wake waiter")
	}
}

func TestWindowRejectsUnknownStreamAndBoundsCredit(t *testing.T) {
	w := NewWindowManager(4, 8)
	if err := w.AddStream(1, 3); err != nil {
		t.Fatal(err)
	}
	if err := w.Grant(1, 6); err == nil {
		t.Fatal("accepted over-cap credit")
	}
	if err := w.Acquire(context.Background(), 3, 1); !errors.Is(err, ErrUnknownStream) {
		t.Fatalf("unknown stream err=%v", err)
	}
}

func TestWindowRemovedStreamWakesWaiter(t *testing.T) {
	w := NewWindowManager(1, 8)
	if err := w.AddStream(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := w.Acquire(context.Background(), 1, 1); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- w.Acquire(context.Background(), 1, 1) }()
	time.Sleep(10 * time.Millisecond)
	w.RemoveStream(1)
	select {
	case err := <-result:
		if !errors.Is(err, ErrUnknownStream) {
			t.Fatalf("removed stream err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("removing stream did not wake blocked writer")
	}
}
