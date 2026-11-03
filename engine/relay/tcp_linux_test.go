//go:build linux && (amd64 || arm64)

package relay

import (
	"io"
	"net"
	"testing"
)

func TestTCPPlatformSelectsSpliceForBareTCP(t *testing.T) {
	left, leftPeer := tcpPair(t)
	right, rightPeer := tcpPair(t)
	if _, ok := left.(*net.TCPConn); !ok {
		t.Fatalf("left connection type is %T", left)
	}
	if _, ok := right.(*net.TCPConn); !ok {
		t.Fatalf("right connection type is %T", right)
	}
	if _, err := leftPeer.Write([]byte("splice")); err != nil {
		t.Fatal(err)
	}
	if err := leftPeer.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := copyTCPPlatform(left, right, LeftToRight, nil); err != nil {
		t.Fatalf("splice copy returned error: %v", err)
	}
	got, err := io.ReadAll(rightPeer)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "splice" {
		t.Fatalf("destination payload = %q", got)
	}
	_ = leftPeer.Close()
	_ = rightPeer.Close()
}
