package relay

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"testing"
)

func BenchmarkTCPRelayBareTCP(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("benchmark target is the Linux bare-TCP relay path")
	}
	client, relayLeft := benchmarkTCPPair(b)
	relayRight, target := benchmarkTCPPair(b)
	ctx, cancel := context.WithCancel(context.Background())
	relayDone := make(chan error, 1)
	go func() {
		_, err := TCP(ctx, relayLeft, relayRight, nil)
		relayDone <- err
	}()

	const payloadSize = 64 * 1024
	payload := make([]byte, payloadSize)
	totalBytes := int64(b.N) * payloadSize
	sinkDone := make(chan error, 1)
	go func() {
		_, err := io.CopyN(io.Discard, target, totalBytes)
		sinkDone <- err
	}()
	b.SetBytes(payloadSize)
	b.ResetTimer()
	for range b.N {
		if err := writeAll(client, payload); err != nil {
			b.Fatal(err)
		}
	}
	if err := client.CloseWrite(); err != nil {
		b.Fatal(err)
	}
	if err := <-sinkDone; err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
	cancel()
	_ = client.Close()
	_ = target.Close()
	if err := <-relayDone; err != nil && ctx.Err() == nil {
		b.Fatal(err)
	}
}

func benchmarkTCPPair(b *testing.B) (*net.TCPConn, *net.TCPConn) {
	b.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		b.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	if err != nil {
		_ = client.Close()
		b.Fatal(err)
	}
	return client, server
}

func writeAll(conn *net.TCPConn, payload []byte) error {
	for len(payload) > 0 {
		n, err := conn.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("benchmark: zero-byte TCP write")
		}
	}
	return nil
}
