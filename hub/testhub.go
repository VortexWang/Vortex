package hub

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

type Message struct {
	Type     string `json:"type"`
	NodeID   string `json:"node_id,omitempty"`
	BootID   string `json:"boot_id,omitempty"`
	Version  uint64 `json:"version,omitempty"`
	Sequence uint64 `json:"sequence,omitempty"`
	ID       string `json:"id,omitempty"`
}

// TestServer is intentionally a protocol-test service, not the Phase 6 backend.
type TestServer struct {
	controlv1.UnimplementedNodeControlServer
	Store               *store.Store
	ConfigUpdate        *controlv1.RuntimeConfigUpdate
	AuthorizationUpdate func(nodeID, bootID string) (*controlv1.AuthorizationUpdate, error)
	UsageBatchLimit     int
	AcceptUsage         bool
	Usage               *UsageRelay
	mu                  sync.RWMutex
	revoked             map[string]struct{}
}

func (s *TestServer) Revoke(serial string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked == nil {
		s.revoked = make(map[string]struct{})
	}
	s.revoked[serial] = struct{}{}
}

func (s *TestServer) isRevoked(cert *x509.Certificate) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.revoked[cert.SerialNumber.String()]
	return ok
}

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

// ServeGRPC runs the contract-defined mTLS control service used by the node
// process tests. It is not a production Hub authority.
func (s *TestServer) ServeGRPC(ctx context.Context, ln net.Listener, config *tls.Config) error {
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(config.Clone())))
	controlv1.RegisterNodeControlServer(server, s)
	go func() {
		<-ctx.Done()
		server.Stop()
	}()
	err := server.Serve(ln)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (s *TestServer) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	ctx := stream.Context()
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return fmt.Errorf("test hub: control peer identity missing")
	}
	tlsInfo, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return fmt.Errorf("test hub: control mTLS certificate missing")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	if certificate.Subject.CommonName == "" || len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != certificate.Subject.CommonName || s.isRevoked(certificate) {
		return fmt.Errorf("test hub: control identity rejected")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetNodeId() != certificate.DNSNames[0] || first.GetBootId() == "" || first.GetSequence() == 0 || first.GetHeartbeat() == nil {
		return fmt.Errorf("test hub: control session identity or first heartbeat invalid")
	}
	receiptID := fmt.Sprintf("control/%s/%s/%d", first.GetNodeId(), first.GetBootId(), first.GetSequence())
	if err := s.Store.RecordReceipt(ctx, receiptID, first.GetSequence()); err != nil {
		return err
	}
	if s.ConfigUpdate == nil && s.AuthorizationUpdate == nil && !s.AcceptUsage {
		return nil
	}
	usageReceived := 0
	usageSeen := make(map[string]bool)
	var update *controlv1.RuntimeConfigUpdate
	controlSequence := uint64(0)
	if s.ConfigUpdate != nil {
		messageID, err := testHubMessageID()
		if err != nil {
			return err
		}
		update = proto.Clone(s.ConfigUpdate).(*controlv1.RuntimeConfigUpdate)
		if err := stream.Send(&controlv1.ControlMessage{
			MessageId: messageID,
			Sequence:  1,
			Body:      &controlv1.ControlMessage_ConfigUpdate{ConfigUpdate: update},
		}); err != nil {
			return err
		}
		controlSequence = 1
	}
	if s.AuthorizationUpdate != nil {
		authorization, err := s.AuthorizationUpdate(first.GetNodeId(), first.GetBootId())
		if err != nil {
			return err
		}
		messageID, err := testHubMessageID()
		if err != nil {
			return err
		}
		controlSequence++
		if err := stream.Send(&controlv1.ControlMessage{MessageId: messageID, Sequence: controlSequence, Body: &controlv1.ControlMessage_AuthorizationUpdate{AuthorizationUpdate: authorization}}); err != nil {
			return err
		}
		if s.ConfigUpdate == nil && !s.AcceptUsage {
			return nil
		}
	}
	lastNodeSequence := first.GetSequence()
	for {
		message, err := stream.Recv()
		if err != nil {
			return err
		}
		if message.GetNodeId() != first.GetNodeId() || message.GetBootId() != first.GetBootId() || message.GetSequence() <= lastNodeSequence {
			return fmt.Errorf("test hub: control message identity or sequence invalid")
		}
		lastNodeSequence = message.GetSequence()
		switch body := message.GetBody().(type) {
		case *controlv1.NodeMessage_UnknownUsageBatch:
			payload, err := contracts.EncodeUnknownUsage(body.UnknownUsageBatch)
			if err != nil {
				return err
			}
			if s.Usage != nil {
				err = s.Usage.AcceptUnknown(ctx, first.GetNodeId(), body.UnknownUsageBatch)
			} else {
				err = s.Store.PersistUnknownUsage(ctx, first.GetNodeId(), payload)
			}
			if err != nil {
				return err
			}
			digest := sha256.Sum256(payload)
			ackID, err := testHubMessageID()
			if err != nil {
				return err
			}
			controlSequence++
			if err := stream.Send(&controlv1.ControlMessage{MessageId: ackID, Sequence: controlSequence, Body: &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: body.UnknownUsageBatch.GetBatchId(), PayloadSha256: digest[:]}}}); err != nil {
				return err
			}
			if !usageSeen[body.UnknownUsageBatch.GetBatchId()] {
				usageSeen[body.UnknownUsageBatch.GetBatchId()] = true
				usageReceived++
			}
			if s.UsageBatchLimit <= usageReceived {
				return nil
			}
		case *controlv1.NodeMessage_UsageBatch:
			batch := body.UsageBatch
			if batch == nil || batch.GetBatchId() == "" || batch.GetBillingSnapshotId() == "" {
				return fmt.Errorf("test hub: usage batch identity is invalid")
			}
			payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(batch)
			if err != nil {
				return err
			}
			if s.Usage != nil {
				err = s.Usage.AcceptKnown(ctx, first.GetNodeId(), message.GetBootId(), batch)
			} else {
				err = s.Store.PersistUsageLedger(ctx, store.UsageBatch{NodeID: first.GetNodeId(), BootID: message.GetBootId(), BatchID: batch.GetBatchId(), SnapshotID: batch.GetBillingSnapshotId(), Payload: payload})
			}
			if err != nil {
				return err
			}
			ackID, err := testHubMessageID()
			if err != nil {
				return err
			}
			controlSequence++
			if err := stream.Send(&controlv1.ControlMessage{MessageId: ackID, Sequence: controlSequence, Body: &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{batch.GetBatchId()}}}}); err != nil {
				return err
			}
			if !usageSeen[batch.GetBatchId()] {
				usageSeen[batch.GetBatchId()] = true
				usageReceived++
			}
			if s.UsageBatchLimit <= usageReceived {
				return nil
			}
		case *controlv1.NodeMessage_ApplyResult:
			if update == nil || body.ApplyResult == nil || !bytesEqual(body.ApplyResult.GetTransactionId(), update.GetTransactionId()) {
				return fmt.Errorf("test hub: config result does not correlate with update")
			}
			payload, err := proto.Marshal(body.ApplyResult)
			if err != nil {
				return err
			}
			return s.Store.AppendEvent(ctx, "apply-"+hex.EncodeToString(update.GetTransactionId()), "control-apply-result", payload)
		default:
			return fmt.Errorf("test hub: unsupported node control message")
		}
	}
}

func testHubMessageID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := range left {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
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
		if tlsConn, ok := conn.(*tls.Conn); ok {
			state := tlsConn.ConnectionState()
			if in.Type != "hello" || in.BootID == "" || len(state.PeerCertificates) == 0 || len(state.PeerCertificates[0].DNSNames) != 1 || state.PeerCertificates[0].DNSNames[0] != in.NodeID || s.isRevoked(state.PeerCertificates[0]) {
				_ = enc.Encode(Message{Type: "error", ID: in.ID})
				return
			}
		}
		if in.ID == "" {
			in.ID = fmt.Sprintf("%s-%s-%d", in.Type, in.NodeID, in.Version)
		}
		if err := s.Store.RecordReceipt(ctx, in.ID, in.Sequence); err != nil {
			_ = enc.Encode(Message{Type: "error", ID: in.ID})
			return
		}
		out := Message{Type: "ack", ID: in.ID, Version: in.Version, Sequence: in.Sequence}
		if strings.EqualFold(in.Type, "hello") {
			out.Type = "config"
		}
		if err := enc.Encode(out); err != nil {
			return
		}
	}
}
