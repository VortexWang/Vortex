package phase5_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent"
	"vortex/agent/store"
	"vortex/hub"
)

func TestStartStopTracerUsesSeparateDurableStores(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	agentStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = (&hub.TestServer{Store: hubStore}).Serve(ctx, ln) }()
	node, err := agent.New("node-1", agentStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(map[string]any{"type": "hello", "node_id": node.NodeID, "boot_id": node.BootID, "id": "tracer-hello", "version": uint64(1)}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	if err := node.Heartbeat(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := hubStore.ReceiptCount(ctx); err != nil || n != 1 {
		t.Fatalf("hub receipts=%d err=%v", n, err)
	}
	if n, err := agentStore.EventCount(ctx); err != nil || n != 2 {
		t.Fatalf("agent events=%d err=%v", n, err)
	}
	if err := node.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
