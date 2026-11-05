package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

func TestProtocolTestHubPersistsHelloReceipt(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = (&TestServer{Store: s}).Serve(ctx, ln) }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	if err := enc.Encode(Message{Type: "hello", NodeID: "n1", BootID: "b1", ID: "m1", Version: 1}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil || len(line) == 0 {
		t.Fatalf("ack: %v", err)
	}
	var ack Message
	if err := json.Unmarshal(line, &ack); err != nil || ack.Type != "config" || ack.ID != "m1" {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		n, countErr := s.ReceiptCount(context.Background())
		if countErr == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("receipt count=%d err=%v", n, countErr)
		}
	}
}
