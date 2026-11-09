package relay

import (
	"net"
	"testing"
)

func TestTuneUDPBuffersAcceptsUsableSocket(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := TuneUDPBuffers(conn); err != nil {
		t.Fatalf("tune UDP buffers: %v", err)
	}
}

func TestTuneUDPBuffersRejectsNilSocket(t *testing.T) {
	if err := TuneUDPBuffers(nil); err == nil {
		t.Fatal("expected nil UDP connection to be rejected")
	}
}
