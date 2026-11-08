package hub

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// BackendClient implements the liveness-only slice of the existing HubControl
// contract. A fresh Hub remains blocked with no signer. This client does not
// accept allocation/restriction/handoff bodies until their authority consumer
// is installed; unsupported bodies terminate the stream without granting rights.
type BackendClient struct {
	HubID, BootID, BackendID, Region, Address string
	TLS                                       *tls.Config // must use a backend-role root, never the node root
	Store                                     *store.Store
	Clock                                     func() store.IssuerTime
	RecoveryPreflight                         bool // independent candidate handshake before the funded stream
}

func (c *BackendClient) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.Store == nil || c.TLS == nil || c.TLS.InsecureSkipVerify || c.TLS.ServerName != c.BackendID || c.TLS.RootCAs == nil || len(c.TLS.Certificates) == 0 {
		return errors.New("Hub backend identity configuration required")
	}
	for _, id := range []string{c.HubID, c.BootID, c.BackendID} {
		if _, err := uuid.Parse(id); err != nil {
			return err
		}
	}
	cfg := c.TLS.Clone()
	cfg.MinVersion = tls.VersionTLS13
	cfg.MaxVersion = tls.VersionTLS13
	cfg.SessionTicketsDisabled = true
	cfg.ClientSessionCache = nil
	prior := cfg.VerifyConnection
	cfg.VerifyConnection = func(state tls.ConnectionState) error {
		if prior != nil {
			if err := prior(state); err != nil {
				return err
			}
		}
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return errors.New("backend identity unavailable")
		}
		cert := state.PeerCertificates[0]
		if len(cert.DNSNames) != 1 || cert.DNSNames[0] != c.BackendID || cert.Subject.CommonName != c.BackendID {
			return errors.New("wrong backend role identity")
		}
		return nil
	}
	conn, err := grpc.DialContext(ctx, c.Address, grpc.WithBlock(), grpc.WithTransportCredentials(credentials.NewTLS(cfg)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := controlv1.NewHubControlClient(conn).Connect(ctx)
	if err != nil {
		return err
	}
	if c.RecoveryPreflight {
		available, e := c.runRecoveryPreflight(ctx, stream)
		if e != nil {
			return e
		}
		if !available {
			return ErrRecoveryEvidenceRetained
		}
		if e = stream.CloseSend(); e != nil {
			return e
		}
		stream, e = controlv1.NewHubControlClient(conn).Connect(ctx)
		if e != nil {
			return e
		}
	}
	if c.Clock != nil {
		return c.runFunded(ctx, stream)
	}
	session := uuid.NewString()
	sequence, err := c.Store.NextControlSequence(ctx)
	if err != nil {
		return err
	}
	if err = stream.Send(&controlv1.HubMessage{HubId: c.HubID, BootId: c.BootID, Sequence: sequence, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, SessionId: session, RegionId: c.Region}}}); err != nil {
		return err
	}
	message, err := stream.Recv()
	if err != nil {
		return err
	}
	hb := message.GetHeartbeat()
	if message.Sequence == 0 || message.MessageId == "" || hb == nil || hb.BackendId != c.BackendID || hb.HubId != c.HubID || hb.SessionId != session || hb.RegionId != c.Region {
		return errors.New("backend heartbeat binding mismatch")
	}
	observed, err := time.Parse(time.RFC3339Nano, hb.ObservedAt)
	if err != nil {
		return err
	}
	deadline, err := time.Parse(time.RFC3339Nano, hb.AutonomyDeadline)
	if err != nil {
		return err
	}
	now := time.Now()
	if observed.After(now) || now.Sub(observed) > 5*time.Second || !deadline.After(observed) || deadline.After(observed.Add(12*time.Hour)) {
		return errors.New("backend time outside conservative bounds")
	}
	if err = c.Store.RecordBackendHeartbeat(ctx, observed, deadline); err != nil {
		return err
	}
	// Read back FULL-committed state before publishing the status.
	persisted, err := c.Store.CurrentAutonomyStatus(ctx, now, true)
	if err != nil {
		return err
	}
	snapshot, err := c.Store.HubControlSnapshot(ctx, c.HubID, now)
	if err != nil {
		return err
	}
	if snapshot.Epoch != 0 {
		return errors.New("liveness-only adapter requires unprovisioned blocked Hub")
	}
	sequence, err = c.Store.NextControlSequence(ctx)
	if err != nil {
		return err
	}
	status := &controlv1.HubStatus{HubId: c.HubID, RegionId: c.Region, Mode: "blocked", SessionId: session, ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, BackendHeartbeatAt: persisted.BackendHeartbeat.UTC().Format(time.RFC3339Nano), AutonomyDeadline: persisted.HardDeadline.UTC().Format(time.RFC3339Nano)}
	if err = stream.Send(&controlv1.HubMessage{HubId: c.HubID, BootId: c.BootID, Sequence: sequence, Body: &controlv1.HubMessage_Status{Status: status}}); err != nil {
		return err
	}
	// Fail closed on any new body until the full authority adapter is present.
	_, err = stream.Recv()
	if err != nil {
		return err
	}
	return errors.New("unsupported backend authority body")
}
