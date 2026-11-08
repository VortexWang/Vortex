package control

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

var fundedHubCapabilities = []string{"status-v1", "backend-allocation-v1", "backend-ledger-v1"}
var closureHubCapabilities = append(append([]string{}, fundedHubCapabilities...), "share-renewal-close-v1")

var sealHubCapabilities = append(append([]string{}, closureHubCapabilities...), "delegation-seal-v1")

func (h *HubAdapter) connectFunded(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage], id string, hello *controlv1.HubMessage) error {
	body := hello.GetLifecycleHello()
	usageOnly := slices.Equal(body.Capabilities, []string{"status-v1", "usage-ledger-v1"})
	sealCapable := slices.Equal(body.Capabilities, sealHubCapabilities)
	closureCapable := sealCapable || slices.Equal(body.Capabilities, closureHubCapabilities)
	expectedCapabilities := fundedHubCapabilities
	if closureCapable {
		expectedCapabilities = closureHubCapabilities
	}
	if sealCapable {
		expectedCapabilities = sealHubCapabilities
	}
	if usageOnly {
		expectedCapabilities = body.Capabilities
	}
	if h.Shares == nil || h.Observe == nil || hello.HubId != id || hello.Sequence == 0 || !validUUID(hello.BootId) || !validUUID(body.SessionId) || body.RegionId != h.Region || !slices.Equal(body.Capabilities, expectedCapabilities) || len(body.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("invalid funded Hub capability/identity")
	}
	session, e := h.Shares.BindHubSession(stream.Context(), id, hello.BootId, uuid.NewString())
	if e != nil {
		return e
	}
	var sequence uint64
	send := func(m *controlv1.BackendMessage) error {
		sequence++
		m.Sequence, m.MessageId = sequence, uuid.NewString()
		return stream.Send(m)
	}
	var heartbeat *controlv1.BackendHeartbeat
	waitingStatus := false
	statusConfirmed := false
	var statusDue time.Time
	sendHeartbeat := func(s service.HubSession) error {
		heartbeat = &controlv1.BackendHeartbeat{BackendId: h.BackendID, HubId: id, SessionId: body.SessionId, RegionId: h.Region, ObservedAt: s.Earliest.Format(time.RFC3339Nano), AutonomyDeadline: s.Deadline.Format(time.RFC3339Nano)}
		waitingStatus = true
		statusDue = time.Now().Add(15 * time.Second)
		return send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_Heartbeat{Heartbeat: heartbeat}})
	}
	if e = sendHeartbeat(session); e != nil {
		return e
	}
	type incoming struct {
		message *controlv1.HubMessage
		err     error
	}
	ch := make(chan incoming, 1)
	go func() {
		for {
			m, e := stream.Recv()
			select {
			case ch <- incoming{m, e}:
			case <-stream.Context().Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := hello.Sequence
	var pending *generated.HubFundedAllocation
	var allocationSequence uint64
	var pendingClosure *generated.ShareRenewalClosureCommand
	var closureSequence uint64
	sendClosure := func() error {
		if !closureCapable || waitingStatus || pendingClosure != nil || pending != nil {
			return nil
		}
		rows, e := h.Shares.PendingRenewalClosures(stream.Context(), id, hello.BootId)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		pendingClosure = &rows[0]
		if e = send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_ShareRenewalClosure{ShareRenewalClosure: &controlv1.ShareRenewalClosureCommand{CommandId: pendingClosure.ID, Payload: pendingClosure.Payload, PayloadSha256: pendingClosure.PayloadDigest}}}); e != nil {
			return e
		}
		closureSequence = sequence
		return nil
	}
	sendAllocation := func() error {
		if usageOnly || pending != nil || waitingStatus || pendingClosure != nil {
			return nil
		}
		rows, e := h.Shares.PendingHubAllocations(stream.Context(), id, hello.BootId)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		pending = &rows[0]
		if e = send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_Allocation{Allocation: &controlv1.BackendAllocation{AllocationId: pending.AllocationID, Payload: pending.Payload, PayloadSha256: pending.PayloadDigest}}}); e != nil {
			return e
		}
		allocationSequence = sequence
		return nil
	}
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			if waitingStatus {
				if time.Now().After(statusDue) {
					return errors.New("funded status acknowledgement timeout")
				}
				continue
			}
			current, e := h.Shares.BindHubSession(stream.Context(), id, hello.BootId, uuid.NewString())
			if e != nil {
				return e
			}
			if current.Epoch != session.Epoch || !current.Deadline.Equal(session.Deadline) {
				return service.ErrShareUnknown
			}
			if e = sendHeartbeat(current); e != nil {
				return e
			}
		case received := <-ch:
			if received.err != nil {
				return received.err
			}
			m := received.message
			if m.HubId != id || m.BootId != hello.BootId || m.Sequence <= last {
				return errors.New("funded Hub envelope identity/sequence mismatch")
			}
			last = m.Sequence
			if status := m.GetStatus(); status != nil {
				if !waitingStatus || status.HubId != id || status.RegionId != h.Region || status.SessionId != body.SessionId || status.ControlVersion != "1.6.0" || !slices.Equal(status.Capabilities, expectedCapabilities) || status.BackendHeartbeatAt != heartbeat.ObservedAt || status.AutonomyDeadline != heartbeat.AutonomyDeadline || !(status.SignerEpoch == 0 && status.Mode == "blocked" || status.SignerEpoch == uint64(session.Epoch) && status.Mode == "online") {
					return errors.New("funded Hub persisted status mismatch")
				}
				if status.SignerEpoch == 0 {
					hasEvidence, e := generated.New(h.Shares.Pool).HubBootHasIssuerEvidence(stream.Context(), generated.HubBootHasIssuerEvidenceParams{HubID: id, HubBootID: hello.BootId})
					if e != nil {
						return e
					}
					if hasEvidence {
						return service.ErrShareUnknown
					}
				}
				raw, e := proto.MarshalOptions{Deterministic: true}.Marshal(status)
				if e != nil {
					return e
				}
				if e = h.Observe(stream.Context(), id, body.SessionId, raw); e != nil {
					return e
				}
				waitingStatus = false
				statusConfirmed = true
				if e = sendClosure(); e != nil {
					return e
				}
				if e = sendAllocation(); e != nil {
					return e
				}
				continue
			}
			if !statusConfirmed {
				return errors.New("persisted funded status required before ledger")
			}
			r := m.GetLedgerRecord()
			if r == nil || r.LedgerSequence == 0 || r.LedgerSequence > math.MaxInt64 || r.RecordId == "" || len(r.Payload) == 0 || len(r.Payload) > 1<<20 {
				return errors.New("funded Hub ledger envelope invalid")
			}
			digest := sha256.Sum256(r.Payload)
			if !bytes.Equal(digest[:], r.PayloadSha256) {
				return errors.New("funded Hub ledger digest mismatch")
			}
			switch r.Kind {
			case "share-renewal-closed-v1":
				if !closureCapable {
					return errors.New("closure capability not negotiated")
				}
				if pendingClosure == nil || r.RecordId != pendingClosure.ID || r.LedgerSequence != closureSequence {
					return service.ErrShareConflict
				}
				decoded, e := contracts.DecodeShareRenewalClosureReceipt(r.Payload)
				if e != nil || decoded.ID != pendingClosure.ID || decoded.CommandDigest != contracts.SharePayloadDigest(pendingClosure.Payload) {
					return service.ErrShareConflict
				}
				if e = h.Shares.RecordRenewalClosureReceipt(stream.Context(), id, hello.BootId, r.Payload, uuid.NewString()); e != nil {
					return e
				}
				pendingClosure = nil
			case "allocation-installed-v1":
				if usageOnly {
					return errors.New("allocation capability not negotiated")
				}
				if pending == nil || r.RecordId != pending.AllocationID || r.LedgerSequence != allocationSequence || !bytes.Equal(r.Payload, pending.Payload) {
					return service.ErrShareConflict
				}
				if e = h.Shares.RecordHubAllocationReceipt(stream.Context(), id, hello.BootId, r.RecordId, r.PayloadSha256, uuid.NewString()); e != nil {
					return e
				}
				pending = nil
			case "usage-v1":
				if !usageOnly {
					return errors.New("usage capability not negotiated")
				}
				if e = h.ingestHubUsage(stream.Context(), id, r); e != nil {
					return e
				}
			case "delegation-sealed-v1":
				if !sealCapable {
					return errors.New("delegation seal capability not negotiated")
				}
				seal, e := contracts.DecodeDelegationSeal(r.Payload)
				if e != nil || seal.ID != r.RecordId {
					return service.ErrShareConflict
				}
				if e = h.Shares.RecordDelegationSeal(stream.Context(), id, hello.BootId, int64(r.LedgerSequence), r.Payload, uuid.NewString()); e != nil {
					return e
				}
			case "issue-v1":
				if usageOnly {
					return errors.New("issue capability not negotiated")
				}
				var head struct {
					ID string `json:"update_id"`
				}
				if json.Unmarshal(r.Payload, &head) != nil || head.ID != r.RecordId {
					return service.ErrShareConflict
				}
				if e = h.Shares.RecordHubIssue(stream.Context(), id, hello.BootId, int64(r.LedgerSequence), r.Payload, uuid.NewString()); e != nil {
					return e
				}
			default:
				return errors.New("unsupported funded Hub ledger kind")
			}
			if e = send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_LedgerAck{LedgerAck: &controlv1.LedgerAcknowledgement{RecordId: r.RecordId, LedgerSequence: r.LedgerSequence, Kind: r.Kind, PayloadSha256: r.PayloadSha256}}}); e != nil {
				return e
			}
			if e = sendClosure(); e != nil {
				return e
			}
			if e = sendAllocation(); e != nil {
				return e
			}
		}
	}
}

func validUUID(id string) bool { parsed, e := uuid.Parse(id); return e == nil && parsed.String() == id }
