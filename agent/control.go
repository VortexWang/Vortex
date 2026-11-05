package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
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
	UsageInterval    time.Duration
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tlsConfig, err := c.tlsConfig()
	if err != nil {
		return err
	}
	if tlsConfig.MinVersion > tls.VersionTLS13 || tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tls.VersionTLS13 {
		return fmt.Errorf("agent: control stream requires TLS 1.3")
	}
	if tlsConfig.InsecureSkipVerify {
		return fmt.Errorf("agent: unauthenticated control TLS is forbidden")
	}
	var issuerIdentity atomic.Value
	previousVerify := tlsConfig.VerifyConnection
	tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
		if previousVerify != nil {
			if err := previousVerify(state); err != nil {
				return err
			}
		}
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return fmt.Errorf("agent: verified Hub identity missing")
		}
		cert := state.PeerCertificates[0]
		if len(cert.DNSNames) != 1 || cert.Subject.CommonName != cert.DNSNames[0] {
			return fmt.Errorf("agent: ambiguous Hub identity")
		}
		issuerIdentity.CompareAndSwap(nil, cert.DNSNames[0])
		if issuerIdentity.Load().(string) != cert.DNSNames[0] {
			return fmt.Errorf("agent: Hub identity changed within control session")
		}
		return nil
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
	initialSendErr := c.sendPendingUsage(ctx, stream, &lastNodeSequence)
	if initialSendErr != nil && !errors.Is(initialSendErr, io.EOF) {
		return initialSendErr
	}
	lastControlSequence := uint64(0)
	seenControlIDs := make(map[string]struct{})
	// Only this loop sends messages; gRPC permits one concurrent receiver, but
	// not multiple senders. Periodic outbox delivery shares sequence ordering
	// with config responses and never races an authorization update.
	type received struct {
		message *controlv1.ControlMessage
		err     error
	}
	incoming := make(chan received)
	go func() {
		for {
			message, err := stream.Recv()
			select {
			case incoming <- received{message, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	usageInterval := c.UsageInterval
	if usageInterval <= 0 {
		usageInterval = 10 * time.Second
	}
	ticker := time.NewTicker(usageInterval)
	defer ticker.Stop()
	if errors.Is(initialSendErr, io.EOF) {
		ticker.Stop()
	}
	for {
		var response received
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.sendPendingUsage(ctx, stream, &lastNodeSequence); err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				// Send EOF can race the peer's final successful ACK. Drain
				// receive messages before completing this control connection.
				ticker.Stop()
			}
			continue
		case response = <-incoming:
		}
		message, recvErr := response.message, response.err
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
		case *controlv1.ControlMessage_AuthorizationUpdate:
			issuer, ok := issuerIdentity.Load().(string)
			if !ok || issuer == "" {
				return fmt.Errorf("agent: authenticated Hub identity missing")
			}
			if err := c.applyAuthorizationUpdateFrom(ctx, body.AuthorizationUpdate, issuer); err != nil {
				return err
			}
			payload, err := validateAuthorizationUpdate(body.AuthorizationUpdate.GetPayload(), c.Node.NodeID, c.Node.BootID, body.AuthorizationUpdate.GetGeneration())
			if err != nil {
				return err
			}
			if c.Node.clock == nil {
				return fmt.Errorf("agent: boot-local authority clock missing")
			}
			if err := c.Node.clock.observe(payload.IssuedAt, c.Node.bootStarted, time.Now()); err != nil {
				return err
			}
		case *controlv1.ControlMessage_UsageAck:
			if body.UsageAck == nil || len(body.UsageAck.GetBatchIds()) == 0 {
				return fmt.Errorf("agent: usage acknowledgement has no batch IDs")
			}
			if err := c.Node.Store.AcknowledgeUsageBatches(ctx, c.Node.NodeID, body.UsageAck.GetBatchIds()); err != nil {
				return err
			}
			// Notify only after durable validation. In particular an ordinary
			// ACK must never expose unknown batches as acknowledged to a hook.
			if c.AcknowledgeUsage != nil {
				if err := c.AcknowledgeUsage(ctx, body.UsageAck.GetBatchIds()); err != nil {
					return err
				}
			}
		case *controlv1.ControlMessage_UnknownUsageAck:
			if body.UnknownUsageAck == nil {
				return fmt.Errorf("agent: missing unknown usage acknowledgement")
			}
			if err := c.Node.Store.AcknowledgeUnknownUsage(ctx, c.Node.NodeID, body.UnknownUsageAck.GetBatchId(), body.UnknownUsageAck.GetPayloadSha256()); err != nil {
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

func (c *ControlClient) applyAuthorizationUpdate(ctx context.Context, update *controlv1.AuthorizationUpdate) error {
	return c.applyAuthorizationUpdateFrom(ctx, update, "")
}

func (c *ControlClient) applyAuthorizationUpdateFrom(ctx context.Context, update *controlv1.AuthorizationUpdate, issuer string) error {
	if c == nil || c.Node == nil || update == nil {
		return fmt.Errorf("invalid_authorization: missing authenticated node or update")
	}
	payload, err := validateAuthorizationUpdate(update.GetPayload(), c.Node.NodeID, c.Node.BootID, update.GetGeneration())
	if err != nil {
		return err
	}
	digest := sha256.Sum256(update.GetPayload())
	records := make([]store.AuthorizationRecord, 0, len(payload.Authorizations))
	for _, authorization := range payload.Authorizations {
		record := store.AuthorizationRecord{
			AuthorizationID: authorization.AuthorizationID, ParentAuthorizationID: authorization.ParentAuthorizationID,
			SourceID: authorization.SourceID, Kind: authorization.Kind, RuleID: authorization.RuleID,
			RuleRevision: authorization.RuleRevision, PathGeneration: authorization.PathGeneration,
			SignerEpoch: payload.SignerEpoch, BootID: payload.BootID,
			IssuedAt:          authorization.IssuedAt.UTC().Format(time.RFC3339Nano),
			NotBefore:         authorization.NotBefore.UTC().Format(time.RFC3339Nano),
			ExpiresAt:         authorization.ExpiresAt.UTC().Format(time.RFC3339Nano),
			HardDeadline:      authorization.HardDeadline.UTC().Format(time.RFC3339Nano),
			BillingSnapshotID: authorization.BillingSnapshotID,
		}
		if authorization.AmountBytes != nil {
			record.AmountBytes = *authorization.AmountBytes
		}
		if authorization.RateBPS != nil {
			record.RateBPS = *authorization.RateBPS
		}
		if authorization.Connections != nil {
			record.Connections = *authorization.Connections
		}
		records = append(records, record)
	}
	return c.Node.Store.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{
		IssuerID: issuer,
		NodeID:   c.Node.NodeID, BootID: c.Node.BootID, UpdateID: payload.UpdateID,
		Payload: update.GetPayload(), Digest: digest[:], SignerEpoch: payload.SignerEpoch,
		PathGeneration: payload.PathGeneration, FencingGenerationFloor: payload.FencingGenerationFloor,
		BackendHeartbeat: payload.BackendHeartbeatAt, AutonomyDeadline: payload.AutonomyDeadline,
	}, records, payload.Revocations)
}

func (c *ControlClient) sendPendingUsage(ctx context.Context, stream controlv1.NodeControl_ConnectClient, lastSequence *uint64) error {
	batches, err := c.Node.Store.PendingUsageOutbox(ctx, c.Node.NodeID)
	if err != nil {
		return fmt.Errorf("agent: read durable usage outbox: %w", err)
	}
	for _, record := range batches {
		// The frozen ordinary UsageBatch has no unknown-liability field.
		// Preserve these rows for a typed handoff rather than reporting their
		// reserved upper bound as actual consumption on the old wire path.
		if record.UnknownUpper != 0 {
			if err := c.migrateUnknownUsage(ctx, record); err != nil {
				return err
			}
			continue
		}
		batch := new(controlv1.UsageBatch)
		if err := proto.Unmarshal(record.Payload, batch); err != nil {
			return fmt.Errorf("agent: decode durable usage batch %s: %w", record.BatchID, err)
		}
		if batch.GetBatchId() != record.BatchID || batch.GetBillingSnapshotId() != record.SnapshotID {
			return fmt.Errorf("agent: durable usage batch metadata mismatch")
		}
		sequence, err := c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return fmt.Errorf("agent: allocate usage sequence: %w", err)
		}
		if err := stream.Send(&controlv1.NodeMessage{
			NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: sequence,
			Body: &controlv1.NodeMessage_UsageBatch{UsageBatch: batch},
		}); err != nil {
			return fmt.Errorf("agent: send durable usage batch %s: %w", record.BatchID, err)
		}
		*lastSequence = sequence
	}
	unknown, err := c.Node.Store.PendingUnknownUsage(ctx, c.Node.NodeID)
	if err != nil {
		return err
	}
	for _, record := range unknown {
		batch, err := contracts.DecodeUnknownUsage(record.Payload)
		if err != nil {
			return err
		}
		sequence, err := c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return err
		}
		if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: sequence, Body: &controlv1.NodeMessage_UnknownUsageBatch{UnknownUsageBatch: batch}}); err != nil {
			return err
		}
		*lastSequence = sequence
	}
	return nil
}

func (c *ControlClient) migrateUnknownUsage(ctx context.Context, record store.UsageBatch) error {
	var legacy controlv1.UsageBatch
	if err := proto.Unmarshal(record.Payload, &legacy); err != nil {
		return err
	}
	if legacy.GetBatchId() != record.BatchID || legacy.GetBillingSnapshotId() != record.SnapshotID || len(legacy.GetDeltas()) != 1 {
		return store.ErrUsageBatchConflict
	}
	d := legacy.GetDeltas()[0]
	if d.GetBytes() != record.UnknownUpper {
		return store.ErrUsageBatchConflict
	}
	payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: record.BatchID, OriginBootId: record.BootID, BillingSnapshotId: record.SnapshotID, RuleId: d.GetRuleId(), GrantId: d.GetGrantId(), Role: d.GetRole(), Direction: d.GetDirection(), UnknownUpperBound: record.UnknownUpper})
	if err != nil {
		return err
	}
	return c.Node.Store.MigrateLegacyUnknownUsage(ctx, record, payload)
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
