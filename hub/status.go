package hub

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// lifecycleStatus reports only previously committed authority. This is still
// the explicit phase-5 protocol fixture, not a production backend adapter.
func (s *TestServer) lifecycleStatus(ctx context.Context, hello *controlv1.LifecycleHello) (*controlv1.HubStatus, error) {
	if hello == nil || hello.ControlVersion != "1.3.0" || hello.RegionId != s.Region || !validStatusUUID(hello.SessionId) || !validLifecycleCapabilities(hello.Capabilities, s.RenewalIssuer != nil) || len(hello.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("test hub: invalid lifecycle hello")
	}
	snapshot, err := s.Store.HubControlSnapshot(ctx, s.HubID, time.Now())
	if err != nil {
		return nil, err
	}
	status := &controlv1.HubStatus{HubId: s.HubID, RegionId: s.Region, Mode: snapshot.Mode, SessionId: hello.SessionId, ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, SignerEpoch: snapshot.Epoch, ConfirmedSequence: snapshot.Confirmed, PendingTailRecords: snapshot.Pending}
	status.Capabilities = append([]string(nil), hello.Capabilities...)
	if !snapshot.BackendHeartbeat.IsZero() {
		status.BackendHeartbeatAt = snapshot.BackendHeartbeat.UTC().Format(time.RFC3339Nano)
	}
	if !snapshot.Deadline.IsZero() {
		status.AutonomyDeadline = snapshot.Deadline.UTC().Format(time.RFC3339Nano)
	}
	return status, nil
}

func validLifecycleCapabilities(caps []string, renewal bool) bool {
	return len(caps) == 1 && caps[0] == "status-v1" || renewal && len(caps) == 2 && caps[0] == "status-v1" && caps[1] == "renewal-v1"
}
func validStatusUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func sendHubStatus(stream controlv1.NodeControl_ConnectServer, status *controlv1.HubStatus, sequence *uint64) error {
	id, err := testHubMessageID()
	if err != nil {
		return err
	}
	*sequence++
	return stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: *sequence, Body: &controlv1.ControlMessage_HubStatus{HubStatus: proto.Clone(status).(*controlv1.HubStatus)}})
}
