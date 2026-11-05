package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"vortex/agent/store"
	"vortex/hub"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "test Hub listen address")
	dbPath := flag.String("db", "hub.db", "Hub SQLite path")
	flag.Parse()
	s, err := store.Open(*dbPath, "hub")
	if err != nil {
		panic(err)
	}
	defer s.Close()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("vortex-test-hub listening=%s protocol=test-only\n", ln.Addr())
	if err := (&hub.TestServer{Store: s}).Serve(ctx, ln); err != nil && ctx.Err() == nil {
		panic(err)
	}
}
