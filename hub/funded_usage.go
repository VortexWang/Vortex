package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c BackendUsageClient) acceptFundedUsage(ctx context.Context, stream grpc.BidiStreamingClient[controlv1.HubMessage, controlv1.BackendMessage], b store.HubUsage) ([]byte, error) {
	cfg := c.Config
	caps, session := []string{"status-v1", "usage-ledger-v1"}, uuid.NewString()
	send := func(m *controlv1.HubMessage) error {
		seq, e := cfg.Store.NextControlSequence(ctx)
		if e != nil {
			return e
		}
		m.HubId, m.BootId, m.Sequence = cfg.HubID, cfg.BootID, seq
		return stream.Send(m)
	}
	if e := send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: caps, SessionId: session, RegionId: cfg.Region}}}); e != nil {
		return nil, e
	}
	reply, e := stream.Recv()
	if e != nil {
		return nil, e
	}
	hb := reply.GetHeartbeat()
	if hb == nil || reply.Sequence == 0 || reply.MessageId == "" {
		return nil, errors.New("funded usage heartbeat required")
	}
	if e = cfg.persistFundedHeartbeat(ctx, session, hb); e != nil {
		return nil, e
	}
	now := cfg.Clock()
	if !validFundedClock(now) {
		return nil, store.ErrAuthorityTimeUncertain
	}
	snapshot, e := cfg.Store.HubControlSnapshot(ctx, cfg.HubID, now.Latest)
	if e != nil {
		return nil, e
	}
	if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: cfg.HubID, RegionId: cfg.Region, SessionId: session, ControlVersion: "1.6.0", Capabilities: caps, Mode: snapshot.Mode, SignerEpoch: snapshot.Epoch, BackendHeartbeatAt: hb.ObservedAt, AutonomyDeadline: hb.AutonomyDeadline, ConfirmedSequence: snapshot.Confirmed, PendingTailRecords: snapshot.Pending}}}); e != nil {
		return nil, e
	}
	envelope := UsageEnvelope{Version: 1, NodeID: b.NodeID, OriginBootID: b.BootID, ReportBootID: b.ReportBootID, Sequence: b.Sequence, BatchID: b.BatchID, Kind: b.Kind, Payload: b.Payload, Digest: b.Digest}
	payload, e := json.Marshal(envelope)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(payload)
	if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: b.BatchID, LedgerSequence: b.Sequence, Kind: "usage-v1", Payload: payload, PayloadSha256: digest[:]}}}); e != nil {
		return nil, e
	}
	ackMessage, e := stream.Recv()
	if e != nil {
		return nil, e
	}
	ack := ackMessage.GetLedgerAck()
	if ackMessage.Sequence <= reply.Sequence || ackMessage.MessageId == "" || ack == nil || ack.RecordId != b.BatchID || ack.Kind != "usage-v1" || ack.LedgerSequence != b.Sequence || !bytes.Equal(ack.PayloadSha256, digest[:]) {
		return nil, errors.New("funded usage acknowledgement mismatch")
	}
	return b.Digest, nil
}
