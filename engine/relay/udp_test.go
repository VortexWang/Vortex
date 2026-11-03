package relay

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestUDPForwardsResponsesToOriginatingClientAndMetersWrites(t *testing.T) {
	target, targetAddr := udpSocket(t)
	listener, listenAddr := udpSocket(t)
	client1, _ := udpSocket(t)
	client2, _ := udpSocket(t)

	var meterMu sync.Mutex
	var metered int64
	ctx, cancel := context.WithCancel(context.Background())
	done := runUDP(ctx, listener, targetAddr, UDPOptions{IdleTimeout: time.Second}, func(_ Direction, n int64) error {
		meterMu.Lock()
		metered += n
		meterMu.Unlock()
		return nil
	})
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("UDP relay did not stop during cleanup")
		}
	})

	writeUDP(t, client1, listenAddr, "first")
	gotClient1, gotAddr1 := readUDP(t, target)
	if string(gotClient1) != "first" || !gotAddr1.IP.IsLoopback() {
		t.Fatalf("target received %q from %v", gotClient1, gotAddr1)
	}
	writeUDP(t, target, gotAddr1, "reply-one")
	if got := readUDPFrom(t, client1); string(got) != "reply-one" {
		t.Fatalf("client one response = %q", got)
	}

	writeUDP(t, client2, listenAddr, "second")
	_, gotAddr2 := readUDP(t, target)
	if gotAddr1.String() == gotAddr2.String() {
		t.Fatal("clients shared one upstream session")
	}
	writeUDP(t, target, gotAddr2, "reply-two")
	if got := readUDPFrom(t, client2); string(got) != "reply-two" {
		t.Fatalf("client two response = %q", got)
	}

	wantMetered := int64(len("first") + len("reply-one") + len("second") + len("reply-two"))
	waitFor(t, time.Second, func() bool {
		meterMu.Lock()
		defer meterMu.Unlock()
		return metered == wantMetered
	})
}

func TestUDPDefaultsIdleTimeoutToSixtySeconds(t *testing.T) {
	if DefaultUDPIdleTimeout != 60*time.Second {
		t.Fatalf("default UDP idle timeout = %s", DefaultUDPIdleTimeout)
	}
}

func TestUDPExpiresIdleSessionAndReplacesIt(t *testing.T) {
	target, targetAddr := udpSocket(t)
	listener, listenAddr := udpSocket(t)
	client, _ := udpSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := runUDP(ctx, listener, targetAddr, UDPOptions{IdleTimeout: 120 * time.Millisecond}, nil)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("UDP relay did not stop during cleanup")
		}
	})

	writeUDP(t, client, listenAddr, "before")
	_, oldAddr := readUDP(t, target)
	time.Sleep(180 * time.Millisecond)
	writeUDP(t, target, oldAddr, "stale")
	if err := client.SetReadDeadline(time.Now().Add(80 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ReadFromUDP(make([]byte, 16)); err == nil {
		t.Fatal("response from an expired upstream session reached the client")
	}

	writeUDP(t, client, listenAddr, "after")
	_, newAddr := readUDP(t, target)
	if oldAddr.String() == newAddr.String() {
		t.Fatal("expired UDP session was reused")
	}
}

func TestUDPCancellationClosesListenerAndSessions(t *testing.T) {
	target, targetAddr := udpSocket(t)
	listener, listenAddr := udpSocket(t)
	client, _ := udpSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := runUDP(ctx, listener, targetAddr, UDPOptions{IdleTimeout: time.Second}, nil)
	writeUDP(t, client, listenAddr, "open")
	_, upstreamAddr := readUDP(t, target)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("UDP returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("UDP relay did not stop after cancellation")
	}
	if err := target.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.ReadFromUDP(make([]byte, 8)); err == nil {
		t.Fatal("upstream session remained open after cancellation")
	}
	if err := target.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	writeUDP(t, target, upstreamAddr, "after-cancel")
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ReadFromUDP(make([]byte, 16)); err == nil {
		t.Fatal("response reached the client after cancellation")
	}
}

func TestUDPRejectsNegativeIdleTimeout(t *testing.T) {
	listener, addr := udpSocket(t)
	defer listener.Close()
	if err := UDP(context.Background(), listener, addr, UDPOptions{IdleTimeout: -time.Second}, nil); err == nil {
		t.Fatal("negative idle timeout was accepted")
	}
}

func runUDP(ctx context.Context, listener *net.UDPConn, target *net.UDPAddr, options UDPOptions, meter Meter) <-chan error {
	done := make(chan error, 1)
	go func() { done <- UDP(ctx, listener, target, options, meter) }()
	return done
}

func udpSocket(t *testing.T) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

func writeUDP(t *testing.T, conn *net.UDPConn, addr *net.UDPAddr, payload string) {
	t.Helper()
	if _, err := conn.WriteToUDP([]byte(payload), addr); err != nil {
		t.Fatal(err)
	}
}

func readUDP(t *testing.T, conn *net.UDPConn) ([]byte, *net.UDPAddr) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n], addr
}

func readUDPFrom(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
