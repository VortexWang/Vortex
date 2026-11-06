package testutil

import (
	"net"
	"os"
	"testing"
	"time"
)

func Valkey(t *testing.T) string {
	t.Helper()
	address := os.Getenv("PHASE6_VALKEY_ADDR")
	if address == "" {
		t.Fatal("PHASE6_VALKEY_ADDR required; cache loss integration cannot be skipped")
	}
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	return address
}

// DisconnectedValkey accepts the connection and cuts it before a RESP reply.
// It exercises an actual established-connection loss rather than a fake client.
func DisconnectedValkey(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return listener.Addr().String()
}
