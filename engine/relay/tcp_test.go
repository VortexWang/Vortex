package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTCPPropagatesHalfCloseAndMetersWrittenBytes(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, upstream := tcpPair(t)

	metered := make(map[Direction]int64)
	var meterMu sync.Mutex
	meter := func(direction Direction, n int64) error {
		meterMu.Lock()
		defer meterMu.Unlock()
		metered[direction] += n
		return nil
	}

	relayDone := make(chan error, 1)
	go func() {
		_, err := TCP(context.Background(), relayLeft, relayRight, meter)
		relayDone <- err
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}

	request, err := io.ReadAll(upstream)
	if err != nil {
		t.Fatal(err)
	}
	if string(request) != "request" {
		t.Fatalf("request = %q, want request", request)
	}
	if _, err := upstream.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	if err := upstream.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "response" {
		t.Fatalf("response = %q, want response", response)
	}

	select {
	case err := <-relayDone:
		if err != nil {
			t.Fatalf("relay returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not finish after both half-closes")
	}

	meterMu.Lock()
	defer meterMu.Unlock()
	if metered[LeftToRight] != int64(len("request")) || metered[RightToLeft] != int64(len("response")) {
		t.Fatalf("metered bytes = %#v", metered)
	}
}

func TestTCPContextCancellationClosesBothDirections(t *testing.T) {
	left, leftPeer := tcpPair(t)
	right, rightPeer := tcpPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := TCP(ctx, left, right, nil)
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("relay returned nil after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop after cancellation")
	}
	_ = leftPeer.Close()
	_ = rightPeer.Close()
}

func TestTCPMeterFailureStopsRelay(t *testing.T) {
	client, relayLeft := tcpPair(t)
	relayRight, upstream := tcpPair(t)
	wantErr := errors.New("accounting unavailable")
	done := make(chan error, 1)
	go func() {
		_, err := TCP(context.Background(), relayLeft, relayRight, func(Direction, int64) error {
			return wantErr
		})
		done <- err
	}()

	if _, err := client.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("relay error = %v, want accounting error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop after meter failure")
	}
	_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
	if got, err := io.ReadAll(upstream); err != nil || string(got) != "payload" {
		t.Fatalf("upstream payload = %q, error = %v", got, err)
	}
	if _, err := upstream.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("upstream read after metering failure = %v, want EOF", err)
	}
}

func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
		return nil, nil
	}
	select {
	case server := <-accepted:
		t.Cleanup(func() {
			_ = client.Close()
			_ = server.Close()
		})
		return client, server
	case err := <-acceptErr:
		_ = client.Close()
		t.Fatal(err)
		return nil, nil
	case <-time.After(2 * time.Second):
		_ = client.Close()
		t.Fatal("accept timed out")
		return nil, nil
	}
}
