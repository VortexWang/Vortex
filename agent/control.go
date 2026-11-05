package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type ControlClient struct {
	Node        *Node
	TLS         *tls.Config
	MaxAttempts int
}

type controlWire struct {
	Type    string `json:"type"`
	NodeID  string `json:"node_id"`
	BootID  string `json:"boot_id"`
	ID      string `json:"id"`
	Version uint64 `json:"version"`
}

func (c *ControlClient) Connect(ctx context.Context, address string) error {
	if c == nil || c.Node == nil || c.TLS == nil {
		return fmt.Errorf("agent: control client is incomplete")
	}
	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 5
	}
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<min(attempt, 5)) * 25 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		dialer := tls.Dialer{Config: c.TLS.Clone()}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			last = err
			continue
		}
		err = c.exchange(ctx, conn)
		_ = conn.Close()
		if err == nil {
			return nil
		}
		last = err
	}
	return fmt.Errorf("agent: control reconnect exhausted: %w", last)
}

func (c *ControlClient) exchange(ctx context.Context, conn net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	id := "hello-" + c.Node.BootID
	if err := json.NewEncoder(conn).Encode(controlWire{Type: "hello", NodeID: c.Node.NodeID, BootID: c.Node.BootID, ID: id, Version: 1}); err != nil {
		return err
	}
	var ack controlWire
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&ack); err != nil {
		return err
	}
	if ack.Type != "ack" && ack.Type != "config" {
		return fmt.Errorf("agent: control rejected: %s", ack.Type)
	}
	if ack.ID != id || ack.Version < 1 {
		return fmt.Errorf("agent: control acknowledgement does not match request")
	}
	if err := c.Node.Store.RecordReceipt(ctx, ack.ID, ack.Version); err != nil {
		return err
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
