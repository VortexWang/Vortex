package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// UsageEnvelope is a versioned LedgerRecord payload, not financial authority.
// Both the forwarding Hub identity and the captured Node envelope are verified.
type UsageEnvelope struct {
	Version      int    `json:"version"`
	NodeID       string `json:"node_id"`
	OriginBootID string `json:"origin_boot_id"`
	ReportBootID string `json:"report_boot_id"`
	Sequence     uint64 `json:"sequence"`
	BatchID      string `json:"batch_id"`
	Kind         string `json:"kind"`
	Payload      []byte `json:"payload"`
	Digest       []byte `json:"payload_sha256"`
}
type BackendUsageClient struct{ Config *BackendClient }

func (c BackendUsageClient) AcceptUsage(ctx context.Context, b store.HubUsage) ([]byte, error) {
	cfg := c.Config
	if cfg == nil || cfg.Store == nil || cfg.TLS == nil || cfg.TLS.InsecureSkipVerify || cfg.TLS.ServerName != cfg.BackendID || len(cfg.TLS.Certificates) == 0 || cfg.TLS.RootCAs == nil {
		return nil, errors.New("verified backend identity configuration required")
	}
	if b.Sequence == 0 || b.ReportBootID == "" {
		return nil, store.ErrUsageOriginMissing
	}
	for _, id := range []string{cfg.HubID, cfg.BootID, cfg.BackendID, b.NodeID, b.BootID, b.ReportBootID} {
		if _, e := uuid.Parse(id); e != nil {
			return nil, e
		}
	}
	digest := sha256.Sum256(b.Payload)
	if !bytes.Equal(digest[:], b.Digest) {
		return nil, store.ErrUsageBatchConflict
	}
	tlsConfig := cfg.TLS.Clone()
	tlsConfig.MinVersion = tls.VersionTLS13
	tlsConfig.MaxVersion = tls.VersionTLS13
	tlsConfig.SessionTicketsDisabled = true
	tlsConfig.ClientSessionCache = nil
	prior := tlsConfig.VerifyConnection
	tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
		if prior != nil {
			if e := prior(state); e != nil {
				return e
			}
		}
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return errors.New("backend chain missing")
		}
		cert := state.PeerCertificates[0]
		if len(cert.DNSNames) != 1 || cert.DNSNames[0] != cfg.BackendID || cert.Subject.CommonName != cfg.BackendID {
			return errors.New("backend role identity mismatch")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, e := grpc.DialContext(ctx, cfg.Address, grpc.WithBlock(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	stream, e := controlv1.NewHubControlClient(conn).Connect(ctx)
	if e != nil {
		return nil, e
	}
	session := uuid.NewString()
	seq, e := cfg.Store.NextControlSequence(ctx)
	if e != nil {
		return nil, e
	}
	if e = stream.Send(&controlv1.HubMessage{HubId: cfg.HubID, BootId: cfg.BootID, Sequence: seq, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.3.0", Capabilities: []string{"status-v1", "usage-ledger-v1"}, SessionId: session, RegionId: cfg.Region}}}); e != nil {
		return nil, e
	}
	reply, e := stream.Recv()
	if e != nil {
		return nil, e
	}
	hb := reply.GetHeartbeat()
	if hb == nil || hb.BackendId != cfg.BackendID || hb.HubId != cfg.HubID || hb.SessionId != session || hb.RegionId != cfg.Region {
		return nil, errors.New("backend usage heartbeat binding mismatch")
	}
	observed, e := time.Parse(time.RFC3339Nano, hb.ObservedAt)
	if e != nil {
		return nil, e
	}
	deadline, e := time.Parse(time.RFC3339Nano, hb.AutonomyDeadline)
	if e != nil {
		return nil, e
	}
	if observed.After(time.Now()) || time.Since(observed) > 5*time.Second || !deadline.After(observed) || deadline.After(observed.Add(12*time.Hour)) {
		return nil, errors.New("backend heartbeat time invalid")
	}
	if e = cfg.Store.RecordBackendHeartbeat(ctx, observed, deadline); e != nil {
		return nil, e
	}
	snapshot, e := cfg.Store.HubControlSnapshot(ctx, cfg.HubID, time.Now())
	if e != nil {
		return nil, e
	}
	if snapshot.Epoch != 0 {
		return nil, errors.New("active signer requires full authority transport")
	}
	seq, e = cfg.Store.NextControlSequence(ctx)
	if e != nil {
		return nil, e
	}
	if e = stream.Send(&controlv1.HubMessage{HubId: cfg.HubID, BootId: cfg.BootID, Sequence: seq, Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: cfg.HubID, RegionId: cfg.Region, Mode: "blocked", SessionId: session, ControlVersion: "1.3.0", Capabilities: []string{"status-v1", "usage-ledger-v1"}, BackendHeartbeatAt: hb.ObservedAt, AutonomyDeadline: hb.AutonomyDeadline}}}); e != nil {
		return nil, e
	}
	envelope := UsageEnvelope{Version: 1, NodeID: b.NodeID, OriginBootID: b.BootID, ReportBootID: b.ReportBootID, Sequence: b.Sequence, BatchID: b.BatchID, Kind: b.Kind, Payload: b.Payload, Digest: b.Digest}
	payload, e := json.Marshal(envelope)
	if e != nil {
		return nil, e
	}
	envelopeDigest := sha256.Sum256(payload)
	seq, e = cfg.Store.NextControlSequence(ctx)
	if e != nil {
		return nil, e
	}
	if e = stream.Send(&controlv1.HubMessage{HubId: cfg.HubID, BootId: cfg.BootID, Sequence: seq, Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: b.BatchID, LedgerSequence: b.Sequence, Kind: "usage-v1", Payload: payload, PayloadSha256: envelopeDigest[:]}}}); e != nil {
		return nil, e
	}
	ackMessage, e := stream.Recv()
	if e != nil {
		return nil, e
	}
	ack := ackMessage.GetLedgerAck()
	if ack == nil || ack.RecordId != b.BatchID || ack.LedgerSequence != b.Sequence || ack.Kind != "usage-v1" || !bytes.Equal(ack.PayloadSha256, envelopeDigest[:]) {
		return nil, errors.New("backend durable usage acknowledgement mismatch")
	}
	return b.Digest, nil
}
