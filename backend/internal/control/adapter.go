package control

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

const MaxMessage = 1 << 20

// LoadCommittedArtifact is the mandatory artifact gate before rule transport.
// A digest alone, an uncommitted row or a different revision/node/generation
// cannot authorize control delivery.
func LoadCommittedArtifact(ctx context.Context, pool *pgxpool.Pool, tenant string, b repository.ArtifactBinding) (compiler.Envelope, error) {
	if pool == nil {
		return compiler.Envelope{}, errors.New("artifact authority unavailable")
	}
	return repository.CommittedArtifact(ctx, generated.New(pool), tenant, b)
}

type request struct {
	op     generated.Operation
	result chan outcome
	ctx    context.Context
}
type ruleRequest struct {
	command *controlv1.RuleCommand
	boot    string
	result  chan ruleOutcome
	ctx     context.Context
	proof   *service.PortProof
	scope   service.TenantScope
}
type ruleOutcome struct {
	receipt *controlv1.RuleReceipt
	err     error
}
type outcome struct {
	receipt service.Receipt
	err     error
}
type shareRequest struct {
	command           *controlv1.ShareCommand
	boot, task, token string
	scope             service.TenantScope
	result            chan error
	ctx               context.Context
}
type sourceRequest struct {
	command *controlv1.SourceProvisionCommand
	boot    string
	scope   service.TenantScope
	result  chan error
	ctx     context.Context
}
type session struct {
	recoveryCreditFences chan nodeRecoveryCreditFenceRequest
	bootRecoveries       chan nodeBootRecoveryRequest
	creditCloses         chan creditCloseRequest
	sources              chan sourceRequest
	requests             chan request
	shares               chan shareRequest
	rules                chan ruleRequest
	done                 chan struct{}
}

// DeliverShare returns success only after the authenticated stream handler
// committed the Node receipt through ShareService's audit transaction.
func (a *Adapter) DeliverShare(ctx context.Context, scope service.TenantScope, task, token, node, boot string, c *controlv1.ShareCommand) error {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return errors.New("target disconnected")
	}
	r := shareRequest{c, boot, task, token, scope, make(chan error, 1), ctx}
	select {
	case s.shares <- r:
	case <-s.done:
		return errors.New("target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e := <-r.result:
		return e
	case <-s.done:
		return errors.New("target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Adapter) DeliverRule(ctx context.Context, node, boot string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
	return a.deliverRule(ctx, node, boot, ruleRequest{command: c, boot: boot, result: make(chan ruleOutcome, 1), ctx: ctx})
}

func (a *Adapter) DeliverPortRelease(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.RuleCommand, proof service.PortProof) error {
	if a.Ports == nil {
		return errors.New("port proof authority unavailable")
	}
	r := ruleRequest{command: c, boot: boot, result: make(chan ruleOutcome, 1), ctx: ctx, proof: &proof, scope: scope}
	_, err := a.deliverRule(ctx, node, boot, r)
	return err
}

func (a *Adapter) deliverRule(ctx context.Context, node, boot string, r ruleRequest) (*controlv1.RuleReceipt, error) {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return nil, errors.New("target disconnected")
	}
	select {
	case s.rules <- r:
	case <-s.done:
		return nil, errors.New("target disconnected")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case out := <-r.result:
		return out.receipt, out.err
	case <-s.done:
		return nil, errors.New("target disconnected")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Adapter serves the existing NodeControl contract. Registry entries only
// locate authenticated live streams; PostgreSQL retains every execution fact.
type Adapter struct {
	controlv1.UnimplementedNodeControlServer
	mu       sync.Mutex
	nodes    map[string]*session
	Observe  func(context.Context, string, string, []byte) error
	Ports    *service.PortService
	Shares   *service.ShareService
	Metering *service.MeteringService
}

func (a *Adapter) Deliver(ctx context.Context, op generated.Operation) (service.Receipt, error) {
	if op.TargetKind != "node" {
		return service.Receipt{}, errors.New("unsupported target kind")
	}
	a.mu.Lock()
	s := a.nodes[op.ResourceID]
	a.mu.Unlock()
	if s == nil {
		return service.Receipt{}, errors.New("target disconnected")
	}
	r := request{op: op, result: make(chan outcome, 1), ctx: ctx}
	select {
	case s.requests <- r:
	case <-s.done:
		return service.Receipt{}, errors.New("target disconnected")
	case <-ctx.Done():
		return service.Receipt{}, ctx.Err()
	}
	select {
	case out := <-r.result:
		return out.receipt, out.err
	case <-s.done:
		return service.Receipt{}, errors.New("target disconnected")
	case <-ctx.Done():
		return service.Receipt{}, ctx.Err()
	}
}
func PeerIdentity(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", errors.New("missing peer")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || info.State.Version != tls.VersionTLS13 || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return "", errors.New("verified TLS 1.3 identity required")
	}
	cert := info.State.PeerCertificates[0]
	if len(cert.DNSNames) != 1 || cert.Subject.CommonName != cert.DNSNames[0] {
		return "", errors.New("ambiguous peer identity")
	}
	if _, err := uuid.Parse(cert.DNSNames[0]); err != nil {
		return "", errors.New("canonical peer identity required")
	}
	return cert.DNSNames[0], nil
}
func (a *Adapter) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	id, err := PeerIdentity(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.NodeId != id || first.BootId == "" || first.Sequence == 0 || first.GetHeartbeat() == nil {
		return errors.New("invalid node handshake")
	}
	if _, err = uuid.Parse(first.BootId); err != nil {
		return err
	}
	s := &session{recoveryCreditFences: make(chan nodeRecoveryCreditFenceRequest), bootRecoveries: make(chan nodeBootRecoveryRequest), creditCloses: make(chan creditCloseRequest), sources: make(chan sourceRequest), requests: make(chan request), rules: make(chan ruleRequest), shares: make(chan shareRequest), done: make(chan struct{})}
	a.mu.Lock()
	if a.nodes == nil {
		a.nodes = map[string]*session{}
	}
	if len(a.nodes) >= 4096 || a.nodes[id] != nil {
		a.mu.Unlock()
		return errors.New("node session already connected or capacity reached")
	}
	a.nodes[id] = s
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.nodes, id); close(s.done); a.mu.Unlock() }()
	if a.Observe != nil {
		raw, e := proto.Marshal(first)
		if e != nil {
			return e
		}
		if e = a.Observe(stream.Context(), id, first.BootId, raw); e != nil {
			return e
		}
	}
	incoming := make(chan *controlv1.NodeMessage, 1)
	readError := make(chan error, 1)
	go func() {
		for {
			m, e := stream.Recv()
			if e != nil {
				readError <- e
				return
			}
			select {
			case incoming <- m:
			case <-stream.Context().Done():
				return
			}
		}
	}()
	nodeSequence := first.Sequence
	recoveryCreditFenceCapable := false
	bootRecoveryCapable := false
	creditCloseCapable := false
	sourceCapable := false
	shareCapable := false
	ruleCapable := false
	for _, capability := range first.GetHeartbeat().Capabilities {
		if capability == "node-credit-recovery-v1" {
			recoveryCreditFenceCapable = true
		}
		if capability == "node-boot-recovery-v1" {
			bootRecoveryCapable = true
		}
		if capability == "credit-close-v1" {
			creditCloseCapable = true
		}
		if capability == "source-provision-v1" {
			sourceCapable = true
		}
		if capability == "share-cas-v1" {
			shareCapable = true
		}
		if capability == "rule-two-phase-v1" {
			ruleCapable = true
		}
	}
	var sequence uint64
	var pending *request
	var pendingRule *ruleRequest
	var pendingShare *shareRequest
	var pendingSource *sourceRequest
	var pendingCreditClose *creditCloseRequest
	var pendingFence *nodeRecoveryCreditFenceRequest
	var pendingBootRecovery *nodeBootRecoveryRequest
	for {
		var pendingDone <-chan struct{}
		if pending != nil {
			pendingDone = pending.ctx.Done()
		}
		if pendingRule != nil {
			pendingDone = pendingRule.ctx.Done()
		}
		if pendingShare != nil {
			pendingDone = pendingShare.ctx.Done()
		}
		if pendingSource != nil {
			pendingDone = pendingSource.ctx.Done()
		}
		if pendingCreditClose != nil {
			pendingDone = pendingCreditClose.ctx.Done()
		}
		if pendingBootRecovery != nil {
			pendingDone = pendingBootRecovery.ctx.Done()
		}
		if pendingFence != nil {
			pendingDone = pendingFence.ctx.Done()
		}
		select {
		case <-pendingDone:
			pending = nil
			pendingRule = nil
			pendingShare = nil
			pendingSource = nil
			pendingCreditClose = nil
			pendingBootRecovery = nil
			pendingFence = nil
		case <-stream.Context().Done():
			return stream.Context().Err()
		case e := <-readError:
			return e
		case r := <-s.requests:
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- outcome{err: errors.New("target busy")}
				continue
			}
			if r.ctx.Err() != nil {
				r.result <- outcome{err: r.ctx.Err()}
				continue
			}
			pending = &r
			sequence++
			txid, e := uuid.Parse(r.op.ID)
			if e != nil {
				return e
			}
			update := &controlv1.RuntimeConfigUpdate{Version: uint64(r.op.TargetRevision), ExpectedVersion: uint64(r.op.ExpectedRevision), FencingGeneration: uint64(r.op.Generation), TransactionId: txid[:], Payload: r.op.Payload}
			sum := sha256Bytes(r.op.Payload)
			update.PayloadSha256 = sum
			// Each delivery has a fresh wire message ID; the stable transaction
			// UUID remains the operation ID for durable business deduplication.
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_ConfigUpdate{ConfigUpdate: update}}); err != nil {
				return err
			}
		case r := <-s.rules:
			if !ruleCapable {
				r.result <- ruleOutcome{err: errors.New("target lacks negotiated rule-two-phase-v1 capability")}
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- ruleOutcome{err: errors.New("target busy")}
				continue
			}
			if r.boot != first.BootId {
				r.result <- ruleOutcome{err: errors.New("node incarnation differs from pinned artifact")}
				continue
			}
			if r.ctx.Err() != nil {
				r.result <- ruleOutcome{err: r.ctx.Err()}
				continue
			}
			pendingRule = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_RuleCommand{RuleCommand: r.command}}); err != nil {
				return err
			}
		case r := <-s.recoveryCreditFences:
			if !recoveryCreditFenceCapable || a.Shares == nil {
				r.result <- errors.New("target lacks node-credit-recovery-v1")
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- errors.New("target busy")
				continue
			}
			if r.boot != first.BootId || r.ctx.Err() != nil {
				r.result <- errors.New("recovery credit fence target/request expired")
				continue
			}
			pendingFence = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_RecoveryCreditFenceCommand{RecoveryCreditFenceCommand: r.command}}); err != nil {
				return err
			}
		case r := <-s.bootRecoveries:
			if !bootRecoveryCapable || a.Shares == nil {
				r.result <- errors.New("target lacks node-boot-recovery-v1")
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- errors.New("target busy")
				continue
			}
			if r.boot != first.BootId || r.ctx.Err() != nil {
				r.result <- errors.New("boot recovery target/request expired")
				continue
			}
			pendingBootRecovery = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_NodeBootRecoveryCommand{NodeBootRecoveryCommand: r.command}}); err != nil {
				return err
			}
		case r := <-s.creditCloses:
			if !creditCloseCapable || a.Shares == nil {
				r.result <- errors.New("target lacks credit-close-v1")
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- errors.New("target busy")
				continue
			}
			if r.boot != first.BootId || r.ctx.Err() != nil {
				r.result <- errors.New("credit close target boot/request expired")
				continue
			}
			pendingCreditClose = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_CreditCloseCommand{CreditCloseCommand: r.command}}); err != nil {
				return err
			}
		case r := <-s.sources:
			if !sourceCapable || a.Shares == nil {
				r.result <- errors.New("target lacks source-provision-v1")
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- errors.New("target busy")
				continue
			}
			if r.boot != first.BootId || r.ctx.Err() != nil {
				r.result <- errors.New("source target boot/request expired")
				continue
			}
			pendingSource = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_SourceProvisionCommand{SourceProvisionCommand: r.command}}); err != nil {
				return err
			}
		case r := <-s.shares:
			if !shareCapable || a.Shares == nil {
				r.result <- errors.New("target lacks share-cas-v1 authority")
				continue
			}
			if pending != nil || pendingRule != nil || pendingShare != nil || pendingSource != nil || pendingCreditClose != nil || pendingBootRecovery != nil || pendingFence != nil {
				r.result <- errors.New("target busy")
				continue
			}
			if r.boot != first.BootId || r.ctx.Err() != nil {
				r.result <- errors.New("target boot or request expired")
				continue
			}
			pendingShare = &r
			sequence++
			if err = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_ShareCommand{ShareCommand: r.command}}); err != nil {
				return err
			}
		case message := <-incoming:
			if message.NodeId != id || message.BootId != first.BootId || message.Sequence <= nodeSequence {
				return errors.New("node stream identity or sequence mismatch")
			}
			nodeSequence = message.Sequence
			if receipt := message.GetRecoveryCreditFenceReceipt(); receipt != nil {
				if pendingFence == nil {
					continue
				}
				if len(receipt.Payload) > 1<<20 || len(receipt.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(sha256Bytes(receipt.Payload), receipt.PayloadSha256) {
					return errors.New("invalid recovery credit fence receipt envelope")
				}
				var r store.RecoveryCreditFenceReceipt
				c, e := store.DecodeRecoveryCreditFence(pendingFence.command.Payload)
				if e != nil || json.Unmarshal(receipt.Payload, &r) != nil || r.ID != c.ID || r.NodeID != id || r.BootID != first.BootId || r.Digest != fmt.Sprintf("%x", pendingFence.command.PayloadSha256) {
					return errors.New("recovery credit fence receipt target/command mismatch")
				}
				e = a.Shares.RecordNodeRecoveryCreditFence(stream.Context(), pendingFence.scope, receipt.Payload, uuid.NewString())
				pendingFence.result <- e
				pendingFence = nil
				continue
			}
			if receipt := message.GetNodeBootRecoveryReceipt(); receipt != nil {
				if pendingBootRecovery == nil {
					continue
				}
				if len(receipt.Payload) > 64<<10 || len(receipt.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(sha256Bytes(receipt.Payload), receipt.PayloadSha256) {
					return errors.New("invalid boot recovery receipt envelope")
				}
				var r contracts.NodeBootRecoveryReceipt
				c, e := contracts.DecodeNodeBootRecovery(pendingBootRecovery.command.Payload)
				if e != nil || json.Unmarshal(receipt.Payload, &r) != nil || r.ID != c.ID || r.NodeID != id || r.BootID != first.BootId || r.CommandSHA256 != fmt.Sprintf("%x", pendingBootRecovery.command.PayloadSha256) {
					return errors.New("boot recovery receipt target/command mismatch")
				}
				e = a.Shares.RecordNodeBootRecovery(stream.Context(), pendingBootRecovery.scope, receipt.Payload, uuid.NewString())
				pendingBootRecovery.result <- e
				pendingBootRecovery = nil
				continue
			}
			if receipt := message.GetCreditCloseReceipt(); receipt != nil {
				if pendingCreditClose == nil {
					continue
				}
				if len(receipt.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(sha256Bytes(receipt.Payload), receipt.PayloadSha256) {
					return errors.New("invalid credit close receipt envelope")
				}
				r, e := contracts.DecodeCreditCloseReceipt(receipt.Payload)
				if e != nil {
					return e
				}
				c, e := contracts.DecodeCreditCloseCommand(pendingCreditClose.command.Payload)
				if e != nil || r.CommandID != c.ID || r.CommandSHA256 != fmt.Sprintf("%x", pendingCreditClose.command.PayloadSha256) || r.BackendID != c.BackendID || r.GrantID != c.GrantID || r.NodeID != id || r.BootID != first.BootId {
					return errors.New("credit close receipt target/command mismatch")
				}
				e = a.Shares.RecordCreditClose(stream.Context(), pendingCreditClose.scope, receipt.Payload, uuid.NewString())
				pendingCreditClose.result <- e
				pendingCreditClose = nil
				continue
			}
			if receipt := message.GetSourceProvisionReceipt(); receipt != nil {
				if pendingSource == nil {
					continue
				}
				if len(receipt.Payload) > 16384 || len(receipt.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(sha256Bytes(receipt.Payload), receipt.PayloadSha256) {
					return errors.New("invalid source receipt envelope")
				}
				var observation store.SourceProvisionReceipt
				d := json.NewDecoder(bytes.NewReader(receipt.Payload))
				d.DisallowUnknownFields()
				if e := d.Decode(&observation); e != nil {
					return e
				}
				var extra any
				if d.Decode(&extra) != io.EOF {
					return errors.New("source receipt trailing payload")
				}
				pinned, e := store.DecodeSourceProvision(pendingSource.command.Payload)
				if e != nil {
					return e
				}
				if observation.ID != pinned.ID || observation.Digest != fmt.Sprintf("%x", pendingSource.command.PayloadSha256) || observation.NodeID != id || observation.BootID != first.BootId {
					return errors.New("source receipt target mismatch")
				}
				e = a.Shares.RecordNodeSources(stream.Context(), pendingSource.scope, observation, uuid.NewString())
				pendingSource.result <- e
				pendingSource = nil
				continue
			}
			if receipt := message.GetShareReceipt(); receipt != nil {
				if pendingShare == nil {
					continue
				}
				if !bytes.Equal(sha256Bytes(receipt.Payload), receipt.PayloadSha256) || len(receipt.Payload) > 16384 || len(receipt.ProtoReflect().GetUnknown()) != 0 {
					return errors.New("share receipt digest invalid")
				}
				var observation contracts.ShareObservation
				d := json.NewDecoder(bytes.NewReader(receipt.Payload))
				d.DisallowUnknownFields()
				if e := d.Decode(&observation); e != nil {
					return e
				}
				var extra any
				if d.Decode(&extra) != io.EOF {
					return errors.New("share receipt trailing payload")
				}
				if observation.NodeID != id || observation.BootID != first.BootId {
					return errors.New("share receipt identity invalid")
				}
				e := a.Shares.RecordShareReceipt(stream.Context(), pendingShare.scope, pendingShare.task, pendingShare.token, observation, uuid.NewString())
				pendingShare.result <- e
				pendingShare = nil
				continue
			}
			if batch := message.GetUnknownUsageBatch(); batch != nil {
				if a.Metering == nil {
					return errors.New("usage authority unavailable")
				}
				ack, e := a.Metering.IngestWireUnknown(stream.Context(), id, first.BootId, message.Sequence, batch)
				if e != nil {
					return e
				}
				sequence++
				if e = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: ack.BatchID, PayloadSha256: ack.Digest}}}); e != nil {
					return e
				}
				continue
			}
			if batch := message.GetUsageBatch(); batch != nil {
				if a.Metering == nil {
					return errors.New("usage authority unavailable")
				}
				ack, e := a.Metering.IngestWireUsage(stream.Context(), id, first.BootId, message.Sequence, batch)
				if e != nil {
					return e
				}
				sequence++
				if e = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: []string{ack.BatchID}}}}); e != nil {
					return e
				}
				continue
			}
			if message.GetHeartbeat() != nil {
				continue
			}
			if receipt := message.GetRuleReceipt(); receipt != nil {
				if pendingRule == nil {
					continue
				}
				c := pendingRule.command
				if receipt.Phase != c.Phase || receipt.TaskKey != c.TaskKey || receipt.IdempotencyKey != c.IdempotencyKey || receipt.TargetRevision != c.TargetRevision || receipt.FencingGeneration != c.FencingGeneration || !bytes.Equal(receipt.TransactionId, c.TransactionId) || !bytes.Equal(receipt.ArtifactSha256, c.ArtifactSha256) {
					continue
				}
				var proofErr error
				if pendingRule.proof != nil {
					if receipt.State != "released" {
						proofErr = errors.New("port release execution unknown")
					} else {
						proof := *pendingRule.proof
						proof.Result = "released"
						proofErr = a.Ports.RecordReleaseProof(stream.Context(), pendingRule.scope, proof, uuid.NewString())
					}
				}
				pendingRule.result <- ruleOutcome{receipt: receipt, err: proofErr}
				pendingRule = nil
				continue
			}
			result := message.GetApplyResult()
			if result == nil {
				return errors.New("unexpected node body")
			}
			if pending == nil {
				continue
			} // Late receipt cannot satisfy a different task.
			txid, _ := uuid.Parse(pending.op.ID)
			if !bytes.Equal(result.TransactionId, txid[:]) || result.Version != uint64(pending.op.TargetRevision) || result.FencingGeneration != uint64(pending.op.Generation) {
				continue
			}
			if result.State == "prepared" {
				continue
			}
			state := "unknown"
			switch result.State {
			case "active":
				state = "succeeded"
			case "rejected", "aborted":
				state = "failed"
			}
			raw, e := proto.MarshalOptions{Deterministic: true}.Marshal(result)
			if e != nil {
				return e
			}
			pending.result <- outcome{receipt: service.Receipt{Revision: int64(result.Version), Generation: int64(result.FencingGeneration), State: state, Payload: raw}}
			pending = nil
		}
	}
}

// ServerTLS uses a role-specific client root. Node and Hub listeners must be
// supplied different roots so a valid node cannot impersonate a Hub/backend.
func ServerTLS(certPath, keyPath, caPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid role trust root")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, SessionTicketsDisabled: true}, nil
}

// HubAdapter persists authenticated status and negotiated usage observations.
// It cannot allocate funds or open a signer; plan 06-06 supplies that policy.
type HubAdapter struct {
	controlv1.UnimplementedHubControlServer
	BackendID, HubID, Region string
	Observe                  func(context.Context, string, string, []byte) error
	Metering                 *service.MeteringService
	Shares                   *service.ShareService
}

func (h *HubAdapter) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	id, err := PeerIdentity(stream.Context())
	if err != nil || id != h.HubID {
		return errors.New("untrusted Hub identity")
	}
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	body := hello.GetLifecycleHello()
	if body != nil && body.ControlVersion == "1.6.0" && len(body.Capabilities) == 1 && body.Capabilities[0] == "hub-sr-preflight-v1" {
		return h.connectRecoveryPreflight(stream, id, hello)
	}
	if body != nil && body.ControlVersion == "1.6.0" {
		return h.connectFunded(stream, id, hello)
	}
	if hello.HubId != id || hello.Sequence == 0 || body == nil || body.RegionId != h.Region || body.ControlVersion != "1.3.0" {
		return errors.New("invalid Hub hello")
	}
	if _, err = uuid.Parse(hello.BootId); err != nil {
		return err
	}
	if _, err = uuid.Parse(body.SessionId); err != nil {
		return err
	}
	usageCapable := len(body.Capabilities) == 2 && body.Capabilities[0] == "status-v1" && body.Capabilities[1] == "usage-ledger-v1"
	if (len(body.Capabilities) != 1 || body.Capabilities[0] != "status-v1") && !usageCapable {
		return errors.New("unsupported Hub capability")
	}
	now := time.Now().UTC()
	heartbeat := &controlv1.BackendHeartbeat{BackendId: h.BackendID, HubId: id, SessionId: body.SessionId, RegionId: h.Region, ObservedAt: now.Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}
	if err = stream.Send(&controlv1.BackendMessage{MessageId: uuid.NewString(), Sequence: 1, Body: &controlv1.BackendMessage_Heartbeat{Heartbeat: heartbeat}}); err != nil {
		return err
	}
	reply, err := stream.Recv()
	if err != nil {
		return err
	}
	status := reply.GetStatus()
	if reply.HubId != id || reply.BootId != hello.BootId || reply.Sequence <= hello.Sequence || status == nil || status.HubId != id || status.SessionId != body.SessionId || status.RegionId != h.Region || status.ControlVersion != "1.3.0" || status.Mode != "blocked" || status.SignerEpoch != 0 || status.BackendHeartbeatAt != heartbeat.ObservedAt || status.AutonomyDeadline != heartbeat.AutonomyDeadline {
		return errors.New("Hub status does not confirm persisted heartbeat")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(status)
	if err != nil {
		return err
	}
	if h.Observe == nil {
		return fmt.Errorf("Hub status persistence required")
	}
	if err = h.Observe(stream.Context(), id, body.SessionId, raw); err != nil {
		return err
	}
	if !usageCapable {
		<-stream.Context().Done()
		return stream.Context().Err()
	}
	if h.Metering == nil {
		return errors.New("Hub usage authority unavailable")
	}
	last := reply.Sequence
	for {
		message, e := stream.Recv()
		if e != nil {
			return e
		}
		record := message.GetLedgerRecord()
		if message.HubId != id || message.BootId != hello.BootId || message.Sequence <= last || record == nil || record.Kind != "usage-v1" || record.RecordId == "" || !bytes.Equal(sha256Bytes(record.Payload), record.PayloadSha256) {
			return errors.New("Hub usage envelope identity/digest mismatch")
		}
		last = message.Sequence
		if e = h.ingestHubUsage(stream.Context(), id, record); e != nil {
			return e
		}
		sequence := message.Sequence
		if e = stream.Send(&controlv1.BackendMessage{MessageId: uuid.NewString(), Sequence: sequence, Body: &controlv1.BackendMessage_LedgerAck{LedgerAck: &controlv1.LedgerAcknowledgement{RecordId: record.RecordId, LedgerSequence: record.LedgerSequence, Kind: record.Kind, PayloadSha256: record.PayloadSha256}}}); e != nil {
			return e
		}
	}
}
