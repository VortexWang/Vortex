package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
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
	AdaptiveDemands           []AdaptiveDemandConfig
	EnableCreditDemand        bool // explicit opt-in for migrated experimental demand peers
	DemandInterval            time.Duration
	regionalAttempts          atomic.Uint64
	regionalReady             atomic.Uint64
	regionalReadinessTimeouts atomic.Uint64
	regionalWatchdogTimeouts  atomic.Uint64
	beginMu                   sync.Mutex
	sessionMu                 sync.Mutex
	sessionID                 uint64
	sessionCancel             context.CancelFunc
	currentStatus             *controlv1.HubStatus
	reportMu                  sync.Mutex
	reports                   chan *controlv1.NodeReport
	Node                      *Node
	TLS                       *tls.Config
	TLSProvider               func() (*tls.Config, error)
	CredentialRevocations     *CredentialRevocationView
	Reconciler                *Reconciler
	MaxAttempts               int
	RetryMin                  time.Duration
	RetryMax                  time.Duration
	Interval                  time.Duration
	UsageInterval             time.Duration
	AcknowledgeUsage          func(context.Context, []string) error
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
	Region     string
	Address    string
	HubID      string
	ServerName string
}

func (c *ControlClient) Connect(ctx context.Context, address string) error {
	return c.connect(ctx, HubEndpoint{Address: address}, nil)
}

func (c *ControlClient) connect(ctx context.Context, endpoint HubEndpoint, regional *RegionalConfig) (resultErr error) {
	address := endpoint.Address
	if c == nil || c.Node == nil || (c.TLS == nil && c.TLSProvider == nil) || address == "" {
		return fmt.Errorf("agent: control client or endpoint is incomplete")
	}
	if c.EnableCreditDemand {
		if regional != nil {
			return fmt.Errorf("agent: experimental demand cannot use regional lifecycle")
		}
		// Local monotonic time controls rate refill only. Every write still checks
		// authenticated conservative lease bounds. Reconnect keeps the same epoch;
		// switching from wall bounds retains tokens and earns no downtime credit.
		if err := c.Node.Store.EnableMonotonicRuntimeRate(c.Node.BootID); err != nil {
			return err
		}
	}
	ctx, cancel, incarnation := c.beginSession(ctx)
	defer cancel()
	defer func() {
		c.sessionMu.Lock()
		defer c.sessionMu.Unlock()
		if c.sessionID == incarnation {
			c.currentStatus = nil
		}
	}()
	type receiveFailure struct{ err error }
	var failure atomic.Pointer[receiveFailure]
	defer func() {
		if f := failure.Load(); f != nil && resultErr != nil {
			resultErr = f.err
		}
	}()
	var readiness *time.Timer
	var watchdog *time.Timer
	var restrictedRetry *time.Timer
	defer func() {
		if restrictedRetry != nil {
			restrictedRetry.Stop()
		}
		if watchdog != nil {
			watchdog.Stop()
		}
	}()
	if regional != nil {
		c.regionalAttempts.Add(1)
		readiness = time.AfterFunc(regional.DialTimeout, func() { c.regionalReadinessTimeouts.Add(1); cancel() })
		defer readiness.Stop()
		// Includes a blocked TLS handshake and initial Send/Recv, not healthy lifetime.
	}

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
	var authenticatedTLS atomic.Pointer[tls.ConnectionState]
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
		authenticatedTLS.Store(&state)
		issuerIdentity.CompareAndSwap(nil, cert.DNSNames[0])
		if issuerIdentity.Load().(string) != cert.DNSNames[0] {
			return fmt.Errorf("agent: Hub identity changed within control session")
		}
		return nil
	}
	if endpoint.ServerName != "" {
		tlsConfig.ServerName = endpoint.ServerName
	}
	tlsConfig.MinVersion, tlsConfig.MaxVersion = tls.VersionTLS13, tls.VersionTLS13
	tlsConfig.SessionTicketsDisabled, tlsConfig.ClientSessionCache = true, nil
	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
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
	var negotiated bool
	var session string
	if regional != nil {
		session, err = newControlID()
		if err != nil {
			return err
		}
		sequence, err = c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return err
		}
		caps := []string{"status-v1"}
		if regional.Renewal != nil {
			caps = append(caps, "renewal-v1")
		}
		if err = stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: sequence, Body: &controlv1.NodeMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.3.0", Capabilities: caps, SessionId: session, RegionId: regional.Region}}}); err != nil {
			return err
		}
		lastNodeSequence = sequence
	}

	var initialSendErr error
	var replayGuard controlReplayGuard
	// Only this loop sends messages; gRPC permits one concurrent receiver, but
	// not multiple senders. Periodic outbox delivery shares sequence ordering
	// with config responses and never races an authorization update.
	type received struct {
		message *controlv1.ControlMessage
		err     error
	}
	incoming := make(chan received, maxControlMessageIDs+1)
	receiverDone := make(chan struct{})
	defer func() { cancel(); conn.Close(); <-receiverDone }()
	go func() {
		defer close(receiverDone)
		for {
			message, err := stream.Recv()
			if err == nil && regional == nil {
				err = replayGuard.accept(message.GetSequence(), message.GetMessageId())
				if err == nil {
					var handled bool
					err = c.withSession(ctx, incarnation, func() error {
						if state := authenticatedTLS.Load(); state != nil {
							if err := tlsConfig.VerifyConnection(*state); err != nil {
								return err
							}
						}
						var ackErr error
						handled, ackErr = c.applyUsageAcknowledgement(ctx, message)
						return ackErr
					})
					if handled && err == nil {
						continue
					}
				}
			}
			if err != nil && !errors.Is(err, io.EOF) {
				failure.CompareAndSwap(nil, &receiveFailure{err})
				cancel()
			}
			select {
			case incoming <- received{message, err}:
			case <-ctx.Done():
				return
			default:
				failure.CompareAndSwap(nil, &receiveFailure{fmt.Errorf("agent: control receive backlog limit reached")})
				// A bounded control backlog must fail/reconnect, not deadlock
				// the bidirectional stream while a bulk send awaits peer reads.
				cancel()
				return
			}
			if err != nil {
				return
			}
		}
	}()
	if regional == nil {
		if err := c.sendRecoveryEvents(ctx, stream, &lastNodeSequence); err != nil {
			return err
		}
		initialSendErr = c.sendPendingUsage(ctx, stream, &lastNodeSequence)
		if initialSendErr != nil && !errors.Is(initialSendErr, io.EOF) {
			return initialSendErr
		}
	}
	var demandCursor string
	var demandTicks <-chan time.Time
	var demandWake <-chan struct{}
	var lastDemandWake time.Time
	if c.EnableCreditDemand && regional == nil {
		// Register only scopes bound to this authenticated Hub. A different
		// connection must not consume another Hub's wakeups.
		issuer, _ := issuerIdentity.Load().(string)
		var scopes []AdaptiveDemandConfig
		for _, config := range c.AdaptiveDemands {
			if config.Scope.HubID == issuer {
				scopes = append(scopes, config)
			}
		}
		var release func()
		demandWake, release, err = c.Node.subscribeDemands(scopes)
		if err != nil {
			return err
		}
		defer release()

		interval := c.DemandInterval
		if interval == 0 {
			interval = time.Second
		}
		if interval < 100*time.Millisecond {
			return fmt.Errorf("agent: demand polling interval below bound")
		}
		demandTicker := time.NewTicker(interval)
		defer demandTicker.Stop()
		demandTicks = demandTicker.C
		issuer, _ = issuerIdentity.Load().(string)
		if err := c.sendPendingDemands(ctx, stream, issuer, &demandCursor, &lastNodeSequence); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	usageInterval := c.UsageInterval
	if usageInterval <= 0 {
		usageInterval = 10 * time.Second
	}
	activationTicker := time.NewTicker(100 * time.Millisecond)
	defer activationTicker.Stop()
	ticker := time.NewTicker(usageInterval)
	defer ticker.Stop()
	if errors.Is(initialSendErr, io.EOF) {
		ticker.Stop()
	}
	lastHeartbeat := time.Now()
	lastRenewal := time.Now()
	reports := c.reportQueue()
	for {
		var response received
		select {
		case <-ctx.Done():
			return ctx.Err()
		case report := <-reports:
			if regional == nil || !negotiated {
				continue
			}
			if err := c.withSession(ctx, incarnation, func() error {
				seq, err := c.Node.Store.NextControlSequence(ctx)
				if err != nil {
					return err
				}
				if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: seq, Body: &controlv1.NodeMessage_NodeReport{NodeReport: report}}); err != nil {
					return err
				}
				lastNodeSequence = seq
				return nil
			}); err != nil {
				return err
			}
			continue
		case <-activationTicker.C:
			if state := authenticatedTLS.Load(); state != nil {
				if err := tlsConfig.VerifyConnection(*state); err != nil {
					return err
				}
			}
			if regional != nil && !negotiated {
				continue
			}
			if regional != nil && time.Since(lastHeartbeat) >= regional.Watchdog/3 {
				seq, e := c.Node.Store.NextControlSequence(ctx)
				if e != nil {
					return e
				}
				if e = stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: seq, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}); e != nil {
					return e
				}
				lastNodeSequence = seq
				lastHeartbeat = time.Now()
			}
			if regional != nil && regional.Renewal != nil && time.Since(lastRenewal) >= regional.Renewal.Retry {
				issuer, _ := issuerIdentity.Load().(string)
				if err := c.withSession(ctx, incarnation, func() error {
					if err := c.queueDueRenewals(ctx, issuer, *regional.Renewal); err != nil {
						return err
					}
					return c.sendPendingRenewals(ctx, stream, issuer, &lastNodeSequence)
				}); err != nil {
					return err
				}
				lastRenewal = time.Now()
			}
			if err := c.withSession(ctx, incarnation, func() error { return c.resumePrepared(ctx, stream, &lastNodeSequence) }); err != nil {
				return err
			}
			continue
		case <-demandWake:
			if time.Since(lastDemandWake) < 25*time.Millisecond {
				continue
			}
			lastDemandWake = time.Now()
			demandCursor = ""
			issuer, _ := issuerIdentity.Load().(string)
			if err := c.sendPendingDemands(ctx, stream, issuer, &demandCursor, &lastNodeSequence); err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				demandWake = nil
			}
			continue
		case <-demandTicks:
			issuer, _ := issuerIdentity.Load().(string)
			if err := c.sendPendingDemands(ctx, stream, issuer, &demandCursor, &lastNodeSequence); err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				demandTicks = nil
			}
			continue
		case <-ticker.C:
			if regional != nil && !negotiated {
				continue
			}
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
		if regional != nil && message.GetSequence() > math.MaxInt64 {
			return fmt.Errorf("agent: control sequence overflow")
		}
		if regional != nil {
			if err := replayGuard.accept(message.GetSequence(), message.GetMessageId()); err != nil {
				return err
			}
		}
		if err := func() error {
			c.sessionMu.Lock()
			defer c.sessionMu.Unlock()
			if state := authenticatedTLS.Load(); state != nil {
				if err := tlsConfig.VerifyConnection(*state); err != nil {
					return err
				}
			}
			if c.sessionID != incarnation || ctx.Err() != nil {
				return fmt.Errorf("agent: stale control session")
			}
			if regional != nil {
				if !noUnknownFields(message.ProtoReflect()) {
					return fmt.Errorf("agent: unknown lifecycle field")
				}
				if message.GetHubStatus() == nil && !negotiated {
					return fmt.Errorf("agent: lifecycle operation before readiness")
				}
			}
			switch body := message.GetBody().(type) {
			case *controlv1.ControlMessage_CredentialRevocations:
				if c.CredentialRevocations == nil {
					return fmt.Errorf("agent: credential revocation receiver unavailable")
				}
				issuer, _ := issuerIdentity.Load().(string)
				if err := c.CredentialRevocations.Apply(ctx, issuer, body.CredentialRevocations); err != nil {
					return err
				}
				return nil
			case *controlv1.ControlMessage_HubStatus:
				if regional == nil {
					return fmt.Errorf("agent: unnegotiated Hub status")
				}
				issuer, _ := issuerIdentity.Load().(string)
				if err := validateRegionalStatus(body.HubStatus, endpoint, regional, session, issuer, time.Now()); err != nil {
					return err
				}
				// Status is report data, never an authority clock/funding update.
				c.currentStatus = proto.Clone(body.HubStatus).(*controlv1.HubStatus)
				if body.HubStatus.Mode != "online" && restrictedRetry == nil {
					restrictedRetry = time.AfterFunc(regional.FailoverBudget, cancel)
				}
				if !negotiated {
					negotiated = true
					c.regionalReady.Add(1)
					readiness.Stop()
					watchdog = time.AfterFunc(regional.Watchdog, func() { c.regionalWatchdogTimeouts.Add(1); cancel() })
					if err := c.sendRecoveryEvents(ctx, stream, &lastNodeSequence); err != nil {
						return err
					}
					if err := c.sendPendingUsage(ctx, stream, &lastNodeSequence); err != nil {
						return err
					}
				} else {
					watchdog.Reset(regional.Watchdog)
				}

			case *controlv1.ControlMessage_ConfigUpdate:
				if regional != nil && c.currentStatus.Mode != "online" {
					return fmt.Errorf("agent: config unavailable while Hub is restricted")
				}
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
				if regional != nil && c.currentStatus.Mode != "online" {
					payload, err := validateAuthorizationUpdate(body.AuthorizationUpdate.GetPayload(), c.Node.NodeID, c.Node.BootID, body.AuthorizationUpdate.GetGeneration())
					if err != nil {
						return err
					}
					if len(payload.Authorizations) > 0 || len(payload.PolicyTransitions) > 0 {
						return fmt.Errorf("agent: new grants unavailable while Hub is restricted")
					}
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
				if err := c.resumePrepared(ctx, stream, &lastNodeSequence); err != nil {
					return err
				}

			case *controlv1.ControlMessage_CreditDemandDenied:
				if !c.EnableCreditDemand || regional != nil {
					return fmt.Errorf("agent: unnegotiated experimental demand denial")
				}
				issuer, _ := issuerIdentity.Load().(string)
				if err := c.applyDemandDenial(ctx, issuer, body.CreditDemandDenied); err != nil {
					return err
				}
			case *controlv1.ControlMessage_RenewalResult:
				if regional == nil || regional.Renewal == nil || !negotiated {
					return fmt.Errorf("agent: unnegotiated renewal result")
				}
				issuer, _ := issuerIdentity.Load().(string)
				if err := c.acceptRenewalResult(ctx, issuer, body.RenewalResult); err != nil {
					return err
				}
				if err := c.resumePrepared(ctx, stream, &lastNodeSequence); err != nil {
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
			return nil
		}(); err != nil {
			return err
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
	if payload.RenewalRequestID != "" {
		intent, err := c.Node.Store.RenewalIntent(ctx, payload.RenewalRequestID)
		if err != nil {
			return err
		}
		r := intent.Request
		if intent.IssuerID != issuer || intent.Result == nil || intent.Result.Code != "ok" || intent.Result.AuthorizationUpdate == nil || !bytes.Equal(intent.Result.AuthorizationUpdate.Payload, update.Payload) || hex.EncodeToString(r.RequestSha256) != payload.RenewalRequestSHA256 || r.NodeId != payload.NodeID || r.BootId != payload.BootID || r.PathGeneration != payload.PathGeneration || r.SignerEpoch != payload.SignerEpoch || r.FencingGeneration != payload.FencingGenerationFloor {
			return fmt.Errorf("invalid_authorization: renewal has no matching durable response")
		}
		var grants int
		for _, a := range payload.Authorizations {
			if a.DelegationID != r.DelegationId || a.RuleID != r.RuleId || a.RuleRevision != r.RuleRevision || a.ExpiresAt.Sub(a.IssuedAt) > time.Duration(r.RequestedTtlSeconds)*time.Second {
				return fmt.Errorf("invalid_authorization: renewal expanded requested scope")
			}
			if a.Kind == "credit_grant" {
				grants++
				if a.SourceID != r.SourceId || a.BillingSnapshotID != r.BillingSnapshotId || a.AmountBytes == nil || *a.AmountBytes != r.RequestedRawBytes {
					return fmt.Errorf("invalid_authorization: renewal changed funded source or amount")
				}
			}
		}
		if grants != 1 {
			return fmt.Errorf("invalid_authorization: renewal requires one requested credit grant")
		}
	}
	digest := sha256.Sum256(update.GetPayload())
	records := make([]store.AuthorizationRecord, 0, len(payload.Authorizations))
	for _, authorization := range payload.Authorizations {
		record := store.AuthorizationRecord{
			ShareReferenceID: authorization.ShareReferenceID, DelegationID: authorization.DelegationID,
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
		ShareReferences:   payload.ShareReferences,
		PolicyTransitions: payload.PolicyTransitions,
		IssuerID:          issuer, Handoffs: payload.Handoffs,
		NodeID: c.Node.NodeID, BootID: c.Node.BootID, UpdateID: payload.UpdateID,
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

// ConnectRegion attempts the configured region once, with bounded readiness.
// Runtime callers use RunRegion for watchdog/retry ownership.
func (c *ControlClient) ConnectRegion(ctx context.Context, region string, endpoints []HubEndpoint) error {
	r, err := (RegionalConfig{Region: region, Endpoints: endpoints}).validated()
	if err != nil {
		return err
	}
	var last error
	for _, e := range r.Endpoints {
		last = c.connect(ctx, e, &r)
		if last == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("agent: configured region unavailable: %w", last)
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

func (c *ControlClient) sendRecoveryEvents(ctx context.Context, stream controlv1.NodeControl_ConnectClient, last *uint64) error {
	after := ""
	for {
		events, err := c.Node.Store.RecoveryEvents(ctx, after)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			if len(event.Payload) > 1<<20 {
				return fmt.Errorf("agent: recovery event exceeds payload bound")
			}
			sequence, err := c.Node.Store.NextControlSequence(ctx)
			if err != nil {
				return err
			}
			if err = stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: sequence, Body: &controlv1.NodeMessage_Event{Event: &controlv1.NodeEvent{EventId: event.ID, Kind: "boot-recovery", Payload: event.Payload}}}); err != nil {
				return err
			}
			*last = sequence
			after = event.ID
		}
	}
}

func (c *ControlClient) resumePrepared(ctx context.Context, stream controlv1.NodeControl_ConnectClient, last *uint64) error {
	if c.Reconciler == nil {
		return nil
	}
	prepared, err := c.Node.Store.PreparedConfigs(ctx)
	if err != nil {
		return err
	}
	for _, record := range prepared {
		err = c.Reconciler.ApplyConfig(ctx, record.TransactionID, record.Version, record.FencingGeneration, false)
		if errors.Is(err, ErrActivationPending) {
			continue
		}
		if err != nil {
			return err
		}
		stored, err := c.Node.Store.GetConfigResult(ctx, record.TransactionID)
		if err != nil {
			return err
		}
		sequence, err := c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return err
		}
		if err = stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: sequence, Body: &controlv1.NodeMessage_ApplyResult{ApplyResult: c.Reconciler.readApplyResult(ctx, stored)}}); err != nil {
			return err
		}
		*last = sequence
	}
	return nil
}

// ACK processing is receive-side work: bulk outbound flow control must never
// prevent durable acknowledgement. The receiver validates wire replay first.
func (c *ControlClient) applyUsageAcknowledgement(ctx context.Context, message *controlv1.ControlMessage) (bool, error) {
	switch body := message.GetBody().(type) {
	case *controlv1.ControlMessage_UsageAck:
		if body.UsageAck == nil || len(body.UsageAck.GetBatchIds()) == 0 {
			return true, fmt.Errorf("agent: usage acknowledgement has no batch IDs")
		}
		if err := c.Node.Store.AcknowledgeUsageBatches(ctx, c.Node.NodeID, body.UsageAck.GetBatchIds()); err != nil {
			return true, err
		}
		// Notify only after durable validation. In particular an ordinary
		// ACK must never expose unknown batches as acknowledged to a hook.
		if c.AcknowledgeUsage != nil {
			if err := c.AcknowledgeUsage(ctx, body.UsageAck.GetBatchIds()); err != nil {
				return true, err
			}
		}
	case *controlv1.ControlMessage_UnknownUsageAck:
		if body.UnknownUsageAck == nil {
			return true, fmt.Errorf("agent: missing unknown usage acknowledgement")
		}
		if err := c.Node.Store.AcknowledgeUnknownUsage(ctx, c.Node.NodeID, body.UnknownUsageAck.GetBatchId(), body.UnknownUsageAck.GetPayloadSha256()); err != nil {
			return true, err
		}

	default:
		return false, nil
	}
	return true, nil
}
