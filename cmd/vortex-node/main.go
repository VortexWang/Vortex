package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"vortex/agent"
	"vortex/agent/store"
)

type message struct {
	Type, NodeID, BootID, ID string
	Version                  uint64
}

func main() {
	nodeID := flag.String("node-id", "node-1", "node identity")
	dbPath := flag.String("db", "agent.db", "Agent SQLite path")
	hubAddr := flag.String("hub", "", "protocol-test Hub address")
	flag.Parse()
	s, err := store.Open(*dbPath, "agent")
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	n, err := agent.New(*nodeID, s, nil)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		fatal(err)
	}
	if *hubAddr == "" {
		fmt.Printf("vortex-node node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
		<-ctx.Done()
		return
	}
	conn, err := net.Dial("tcp", *hubAddr)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	dec := bufio.NewScanner(conn)
	_ = enc.Encode(message{Type: "hello", NodeID: n.NodeID, BootID: n.BootID, ID: "hello-" + n.BootID, Version: 1})
	if !dec.Scan() {
		fatal(fmt.Errorf("hub closed before ACK"))
	}
	if err := n.Heartbeat(ctx, 1); err != nil {
		fatal(err)
	}
	fmt.Printf("vortex-node connected node_id=%s boot_id=%s hub_ack=%s\n", n.NodeID, n.BootID, dec.Text())
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
