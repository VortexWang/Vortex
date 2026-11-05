package agent

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	controlv1 "vortex/contracts/protobuf/v1"
)

type ControlClient struct {
	Node             *Node
	TLS              *tls.Config
	TLSProvider      func() (*tls.Config, error)
	Reconciler       *Reconciler
	MaxAttempts      int
	RetryMin         time.Duration
	RetryMax         time.Duration
	Interval         time.Duration
	AcknowledgeUsage func(context.Context, []string) error
}

// Run reconnects after failed or completed control streams until cancellation.
func (c *ControlClient) Run(ctx context.Context, address string) error {
	if c == nil || c.Node == nil || (c.TLS == nil && c.TLSProvider == nil) || address == "" {
		return fmt.Errorf("agent: control client or endpoint is incomplete")
	}
	minimum, maximum := c.retryBounds()
	interval := c.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	delay := minimum
	for {
		err := c.Connect(ctx, address)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			delay = interval
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

func (c *ControlClient) Connect(ctx context.Context, address string) error {
	if c == nil || c.Node == nil || (c.TLS == nil && c.TLSProvider == nil) || address == "" {
		return fmt.Errorf("agent: control client or endpoint is incomplete")
	}
	tlsConfig, err := c.tlsConfig()
	if err != nil {
		return err
	}
	if tlsConfig.MinVersion > tls.VersionTLS13 || tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tls.VersionTLS13 {
		return fmt.Errorf("agent: control stream requires TLS 1.3")
	}
	tlsConfig.MinVersion = tls.VersionTLS13
	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("agent: control TLS/gRPC connect: %w", err)
	}
	defer conn.Close()
	stream, err := controlv1.NewNodeControlClient(conn).Connect(ctx)
	if err != nil {
		return fmt.Errorf("agent: open control stream: %w", err)
	}
	sequence, err := c.Node.Store.NextControlSequence(ctx)
	if err != nil {
		return err
	}
	appliedVersion := uint64(0)
	if raw, stateErr := c.Node.Store.AgentState(ctx, "applied_version"); stateErr == nil {
		appliedVersion, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("agent: stored applied version invalid: %w", err)
		}
	} else if !errors.Is(stateErr, sql.ErrNoRows) {
		return stateErr
	}
	if err := stream.Send(&controlv1.NodeMessage{
		NodeId:   c.Node.NodeID,
		BootId:   c.Node.BootID,
		Sequence: sequence,
		Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{
			AppliedConfigVersion: appliedVersion,
			ObservedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		}},
	}); err != nil {
		return fmt.Errorf("agent: send control heartbeat: %w", err)
	}
	lastNodeSequence := sequence
	lastControlSequence := uint64(0)
	seenControlIDs := make(map[string]struct{})
	for {
		message, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			receiptID, idErr := newControlID()
			if idErr != nil {
				return idErr
			}
			return c.Node.Store.PersistControlReceipt(ctx, receiptID, lastNodeSequence, time.Now().UTC())
		}
		if recvErr != nil {
			return fmt.Errorf("agent: receive control message: %w", recvErr)
		}
		if message.GetSequence() == 0 || message.GetSequence() <= lastControlSequence || !canonicalUUID(message.GetMessageId()) {
			return fmt.Errorf("agent: invalid control message identity or sequence")
		}
		if _, duplicate := seenControlIDs[message.GetMessageId()]; duplicate {
			return fmt.Errorf("agent: duplicate control message id")
		}
		seenControlIDs[message.GetMessageId()] = struct{}{}
		lastControlSequence = message.GetSequence()
		switch body := message.GetBody().(type) {
		case *controlv1.ControlMessage_ConfigUpdate:
			result := c.applyConfig(ctx, body.ConfigUpdate)
			responseSequence, err := c.sendApplyResult(ctx, stream, result)
			if err != nil {
				return err
			}
			lastNodeSequence = responseSequence
		case *controlv1.ControlMessage_UsageAck:
			if c.AcknowledgeUsage == nil {
				return fmt.Errorf("agent: usage acknowledgements are unsupported by this node runtime")
			}
			if err := c.AcknowledgeUsage(ctx, body.UsageAck.GetBatchIds()); err != nil {
				return err
			}
		case *controlv1.ControlMessage_Error:
			if body.Error == nil || body.Error.GetMessageId() != message.GetMessageId() || !stableControlErrorCode(body.Error.GetCode()) {
				return fmt.Errorf("agent: malformed control error")
			}
			return fmt.Errorf("agent: control error %s retryable=%t", body.Error.GetCode(), body.Error.GetRetryable())
		default:
			return fmt.Errorf("agent: unsupported control message body")
		}
	}
}

func stableControlErrorCode(code string) bool {
	switch code {
	case "invalid_config", "unsupported_config", "identity_mismatch", "stale_version", "stale_generation", "invalid_transaction_id", "payload_digest_mismatch", "tx_conflict", "activation_unknown", "prepare_failed", "commit_failed", "aborted":
		return true
	default:
		return false
	}
}

func (c *ControlClient) applyConfig(ctx context.Context, update *controlv1.RuntimeConfigUpdate) *controlv1.ApplyResult {
	if c.Reconciler == nil {
		return &controlv1.ApplyResult{State: "rejected", ErrorCode: "unsupported_config"}
	}
	return c.Reconciler.ApplyRuntimeConfig(ctx, c.Node.NodeID, update)
}

func (c *ControlClient) sendApplyResult(ctx context.Context, stream controlv1.NodeControl_ConnectClient, result *controlv1.ApplyResult) (uint64, error) {
	sequence, err := c.Node.Store.NextControlSequence(ctx)
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, fmt.Errorf("agent: apply handler returned no result")
	}
	if err := stream.Send(&controlv1.NodeMessage{
		NodeId:   c.Node.NodeID,
		BootId:   c.Node.BootID,
		Sequence: sequence,
		Body:     &controlv1.NodeMessage_ApplyResult{ApplyResult: result},
	}); err != nil {
		return 0, fmt.Errorf("agent: send config apply result: %w", err)
	}
	return sequence, nil
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
	for _, address := range candidates {
		if err := c.Connect(ctx, address); err == nil {
			return nil
		} else {
			last = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("agent: all Hub endpoints in region %q failed: %w", region, last)
}

func (c *ControlClient) tlsConfig() (*tls.Config, error) {
	if c.TLSProvider != nil {
		config, err := c.TLSProvider()
		if err != nil {
			return nil, err
		}
		if config == nil {
			return nil, fmt.Errorf("agent: TLS provider returned no configuration")
		}
		return config.Clone(), nil
	}
	if c.TLS == nil {
		return nil, fmt.Errorf("agent: TLS configuration is required")
	}
	return c.TLS.Clone(), nil
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

func newControlID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
