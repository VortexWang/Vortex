package control

import (
	"bytes"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"math"
	"slices"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// This stream retains forensic evidence only. Its typed receipts deliberately
// cannot be consumed as LedgerAcknowledgement by an ordinary funded client.
func (h *HubAdapter) connectRecoveryPreflight(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage], id string, hello *controlv1.HubMessage) error {
	b := hello.GetLifecycleHello()
	if h.Shares == nil || b == nil || hello.HubId != id || !validUUID(hello.BootId) || hello.Sequence == 0 || hello.Sequence > math.MaxInt64 || b.ControlVersion != "1.6.0" || !slices.Equal(b.Capabilities, []string{"hub-sr-preflight-v1"}) || !validUUID(b.SessionId) || b.RegionId != h.Region || len(hello.ProtoReflect().GetUnknown()) != 0 || len(b.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("invalid Hub recovery preflight hello")
	}
	p, available, e := h.Shares.HubRecoveryPreflight(stream.Context(), id, hello.BootId, h.BackendID, uuid.NewString())
	if e != nil {
		return e
	}
	seq := uint64(1)
	send := func(m *controlv1.BackendMessage) error {
		m.MessageId = uuid.NewString()
		m.Sequence = seq
		seq++
		return stream.Send(m)
	}
	if available {
		return send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_Error{Error: &controlv1.ControlError{Code: "funded_session_available"}}})
	}
	if e = send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_RecoveryCommand{RecoveryCommand: &controlv1.HubRecoveryCommand{BackendId: h.BackendID, HubId: id, BootId: hello.BootId, SessionId: b.SessionId, RegionId: h.Region, Payload: p.Payload, PayloadSha256: p.PayloadDigest}}}); e != nil {
		return e
	}
	last := hello.Sequence
	receipt := func(recovery, record, kind string, sequence uint64, digest []byte) error {
		return send(&controlv1.BackendMessage{Body: &controlv1.BackendMessage_RecoveryRetention{RecoveryRetention: &controlv1.HubRecoveryRetention{BackendId: h.BackendID, HubId: id, BootId: hello.BootId, SessionId: b.SessionId, RegionId: h.Region, RecoveryId: recovery, RecordId: record, Kind: kind, LedgerSequence: sequence, PayloadSha256: digest}}})
	}
	for {
		m, e := stream.Recv()
		if e != nil {
			return e
		}
		if m.HubId != id || m.BootId != hello.BootId || m.Sequence <= last || m.Sequence > math.MaxInt64 || len(m.ProtoReflect().GetUnknown()) != 0 {
			return errors.New("Hub recovery preflight identity/sequence mismatch")
		}
		last = m.Sequence
		if r := m.GetRecoveryRecord(); r != nil {
			record := r.Record
			if r.RecoveryId != p.ID || record == nil || len(r.ProtoReflect().GetUnknown()) != 0 || len(record.ProtoReflect().GetUnknown()) != 0 || record.LedgerSequence == 0 || record.LedgerSequence >= math.MaxInt64 || !bytes.Equal(sha256Bytes(record.Payload), record.PayloadSha256) {
				return errors.New("invalid recovery retained record")
			}
			if e = h.Shares.RetainHubSameStoreRecord(stream.Context(), id, hello.BootId, p.ID, store.LedgerRecord{Sequence: int64(record.LedgerSequence), ID: record.RecordId, Kind: record.Kind, Payload: record.Payload, Digest: record.PayloadSha256}, uuid.NewString()); e != nil {
				return e
			}
			if e = receipt(p.ID, record.RecordId, record.Kind, record.LedgerSequence, record.PayloadSha256); e != nil {
				return e
			}
			continue
		}
		if r := m.GetRecoveryManifest(); r != nil {
			if r.RecoveryId != p.ID || len(r.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(sha256Bytes(r.Payload), r.PayloadSha256) {
				return errors.New("invalid recovery manifest envelope")
			}
			witness, e := h.Shares.VerifyHubSameStorePrefix(stream.Context(), id, hello.BootId, p.ID, r.Payload, uuid.NewString())
			if e != nil {
				return e
			}
			return receipt(p.ID, p.ID, "manifest", uint64(witness.SealedSequence+1), witness.PayloadDigest)
		}
		return errors.New("ordinary authority body on recovery preflight stream")
	}
}
