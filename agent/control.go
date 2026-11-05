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
	RetryMin    time.Duration
	RetryMax    time.Duration
}

// Run reconnects after failed or completed one-shot control exchanges until
// the caller cancels. Each exchange uses a new durable sequence number.
func (c *ControlClient) Run(ctx context.Context, address string) error {
	if c == nil || c.Node == nil || c.TLS == nil || address == "" {
		return fmt.Errorf("agent: control client or endpoint is incomplete")
	}
	minimum, maximum := c.retryBounds()
	delay := minimum
	for {
		err := c.Connect(ctx, address)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			delay = minimum
		} else if delay < maximum {
			delay *= 2
			if delay > maximum {
				delay = maximum
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type HubEndpoint struct {
	Region  string
	Address string
}

type controlWire struct {
	Type     string `json:"type"`
	NodeID   string `json:"node_id"`
	BootID   string `json:"boot_id"`
	ID       string `json:"id"`
	Version  uint64 `json:"version"`
	Sequence uint64 `json:"sequence"`
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

// ConnectRegion retries only endpoints explicitly assigned to the requested region.
func (c *ControlClient) ConnectRegion(ctx context.Context, region string, endpoints []HubEndpoint) error {
	var candidates []string
	for _, endpoint := range endpoints {
		if endpoint.Region == region && endpoint.Address != "" {
			candidates = append(candidates, endpoint.Address)
		}
	}
	if len(candidates) == 0 {
		return fmt.Errorf("agent: no Hub endpoints configured for region %q", region)
	}
	var last error
	for i, address := range candidates {
		if err := c.Connect(ctx, address); err == nil {
			return nil
		} else {
			last = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = i
	}
	return fmt.Errorf("agent: all Hub endpoints in region %q failed: %w", region, last)
}

func (c *ControlClient) exchange(ctx context.Context, conn net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	sequence, err := c.Node.Store.NextControlSequence(ctx)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("hello-%s-%d", c.Node.BootID, sequence)
	if err := json.NewEncoder(conn).Encode(controlWire{Type: "hello", NodeID: c.Node.NodeID, BootID: c.Node.BootID, ID: id, Version: 1, Sequence: sequence}); err != nil {
		return err
	}
	var ack controlWire
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&ack); err != nil {
		return err
	}
	if ack.Type != "ack" && ack.Type != "config" {
		return fmt.Errorf("agent: control rejected: %s", ack.Type)
	}
	if ack.ID != id || ack.Sequence != sequence || ack.Version < 1 {
		return fmt.Errorf("agent: control acknowledgement does not match request")
	}
	if err := c.Node.Store.PersistControlReceipt(ctx, ack.ID, ack.Sequence, time.Now().UTC()); err != nil {
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

func (c *ControlClient) retryBounds() (time.Duration, time.Duration) {
	minimum, maximum := c.RetryMin, c.RetryMax
	if minimum <= 0 {
		minimum = 250 * time.Millisecond
	}
	if maximum < minimum {
		maximum = 30 * time.Second
	}
	return minimum, maximum
}
