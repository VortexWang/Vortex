package hub

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"vortex/agent/store"
)

type Message struct {
	Type    string `json:"type"`
	NodeID  string `json:"node_id,omitempty"`
	BootID  string `json:"boot_id,omitempty"`
	Version uint64 `json:"version,omitempty"`
	ID      string `json:"id,omitempty"`
}

// TestServer is intentionally a protocol-test service, not the Phase 6 backend.
type TestServer struct{ Store *store.Store }

func (s *TestServer) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		go s.handle(ctx, conn)
	}
}

func (s *TestServer) ServeTLS(ctx context.Context, ln net.Listener, config *tls.Config) error {
	return s.Serve(ctx, tls.NewListener(ln, config))
}

func (s *TestServer) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)
	for sc.Scan() {
		var in Message
		if json.Unmarshal(sc.Bytes(), &in) != nil {
			_ = enc.Encode(Message{Type: "error"})
			return
		}
		if in.ID == "" {
			in.ID = fmt.Sprintf("%s-%s-%d", in.Type, in.NodeID, in.Version)
		}
		if err := s.Store.RecordReceipt(ctx, in.ID, in.Version); err != nil {
			_ = enc.Encode(Message{Type: "error", ID: in.ID})
			return
		}
		out := Message{Type: "ack", ID: in.ID, Version: in.Version}
		if strings.EqualFold(in.Type, "hello") {
			out.Type = "config"
		}
		if err := enc.Encode(out); err != nil {
			return
		}
	}
}
