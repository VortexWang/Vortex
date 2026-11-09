package control

import (
	"errors"
	"math"
	"slices"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (h *HubAdapter) connectRecoveryAdmission(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage], id string, hello *controlv1.HubMessage) error {
	b := hello.GetLifecycleHello()
	if h.Shares == nil || b == nil || hello.HubId != id || !validUUID(hello.BootId) || hello.Sequence == 0 || hello.Sequence > math.MaxInt64 || b.ControlVersion != "1.6.0" || !slices.Equal(b.Capabilities, []string{"hub-sr-admission-v1"}) || !validUUID(b.SessionId) || b.RegionId != h.Region || len(hello.ProtoReflect().GetUnknown()) != 0 || len(b.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("invalid Hub recovery admission hello")
	}
	raw, available, e := h.Shares.HubRecoveryAdmission(stream.Context(), id, hello.BootId, h.BackendID, uuid.NewString())
	if e != nil {
		return e
	}
	m := &controlv1.BackendMessage{MessageId: uuid.NewString(), Sequence: 1}
	if available {
		m.Body = &controlv1.BackendMessage_Error{Error: &controlv1.ControlError{Code: "funded_session_available"}}
	} else {
		m.Body = &controlv1.BackendMessage_RecoveryAdmission{RecoveryAdmission: &controlv1.HubRecoveryAdmission{BackendId: h.BackendID, HubId: id, BootId: hello.BootId, SessionId: b.SessionId, RegionId: h.Region, Payload: raw, PayloadSha256: sha256Bytes(raw)}}
	}
	return stream.Send(m)
}
