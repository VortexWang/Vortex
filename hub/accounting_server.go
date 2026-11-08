package hub

import (
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// AccountingServer is the production accounting slice of NodeControl. It has
// no allocation/signing policy and cannot issue a runtime or billing lease.
// The fuller control server can compose the same UsageRelay in plan 06-06.
type AccountingServer struct {
	controlv1.UnimplementedNodeControlServer
	Usage *UsageRelay
}

func (s *AccountingServer) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	if s.Usage == nil {
		return errors.New("durable usage relay required")
	}
	p, ok := peer.FromContext(stream.Context())
	if !ok {
		return errors.New("node peer missing")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || info.State.Version != tls.VersionTLS13 || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return errors.New("verified TLS1.3 node identity required")
	}
	cert := info.State.PeerCertificates[0]
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != cert.Subject.CommonName {
		return errors.New("ambiguous node identity")
	}
	node := cert.DNSNames[0]
	if _, e := uuid.Parse(node); e != nil {
		return e
	}
	first, e := stream.Recv()
	if e != nil {
		return e
	}
	if first.NodeId != node || first.Sequence == 0 || first.GetHeartbeat() == nil {
		return errors.New("node accounting handshake mismatch")
	}
	if _, e = uuid.Parse(first.BootId); e != nil {
		return e
	}
	sequence, last := uint64(0), first.Sequence
	for {
		m, e := stream.Recv()
		if e != nil {
			return e
		}
		if m.NodeId != node || m.BootId != first.BootId || m.Sequence <= last || len(m.ProtoReflect().GetUnknown()) != 0 {
			return errors.New("accounting boot/sequence mismatch")
		}
		last = m.Sequence
		if m.GetHeartbeat() != nil {
			continue
		}
		if m.GetUsageBatch() == nil && m.GetUnknownUsageBatch() == nil {
			return errors.New("accounting channel cannot grant authority")
		}
		if e = s.Usage.AcceptEnvelope(stream.Context(), m); e != nil {
			return e
		}
		sequence++
		reply := &controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence}
		if b := m.GetUsageBatch(); b != nil {
			reply.Body = &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{b.BatchId}}}
		} else {
			b := m.GetUnknownUsageBatch()
			payload, e := contracts.EncodeUnknownUsage(b)
			if e != nil {
				return e
			}
			digest := sha256.Sum256(payload)
			reply.Body = &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: b.BatchId, PayloadSha256: digest[:]}}
		}
		if e = stream.Send(reply); e != nil {
			return e
		}
	}
}
