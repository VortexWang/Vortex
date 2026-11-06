package contracts

import (
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// These tests verify generated wire round trips, not runtime capability or
// authority validation. Consumer/process checks belong to the subsequent plans.
func TestLifecycleGeneratedWireRoundTrips(t *testing.T) {
	messages := []proto.Message{
		&controlv1.NodeMessage{NodeId: "node", BootId: "boot", Sequence: 1, Body: &controlv1.NodeMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.3.0", SessionId: "session", RegionId: "region", Capabilities: []string{"renewal-v1"}}}},
		&controlv1.NodeMessage{Sequence: 2, Body: &controlv1.NodeMessage_NodeReport{NodeReport: &controlv1.NodeReport{ReportId: "report", Probes: []*controlv1.ProbeObservation{{TargetId: "target", State: "healthy", Successes: 2}}}}},
		&controlv1.NodeMessage{Sequence: 3, Body: &controlv1.NodeMessage_RenewalRequest{RenewalRequest: &controlv1.RenewalRequest{RequestId: "request", RequestedRawBytes: 1024, RequestedTtlSeconds: 60, RequestSha256: make([]byte, 32)}}},
		&controlv1.ControlMessage{Sequence: 1, Body: &controlv1.ControlMessage_RenewalResult{RenewalResult: &controlv1.RenewalResult{RequestId: "request", Code: "ok", AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: 2, Payload: []byte(`{"schema_version":"1.2.0"}`)}}}},
		&controlv1.ControlMessage{Sequence: 2, Body: &controlv1.ControlMessage_HubStatus{HubStatus: &controlv1.HubStatus{Mode: "recovering", SignerEpoch: 2, RecoveryTargetSequence: 10, ConfirmedSequence: 9, PendingTailRecords: 2, RestrictionRevision: 3}}},
		&controlv1.BackendMessage{Sequence: 1, Body: &controlv1.BackendMessage_AuthorityRestrictions{AuthorityRestrictions: &controlv1.AuthorityRestrictions{Revision: 3, Limits: []*controlv1.DelegationRestriction{{DelegationId: "delegation", MaxTotalIssuedRawBytes: 8192}}}}},
		&controlv1.BackendMessage{Sequence: 2, Body: &controlv1.BackendMessage_EpochHandoffRequest{EpochHandoffRequest: &controlv1.EpochHandoffRequest{FromEpoch: 1, ToEpoch: 2, RequiredRestrictionRevision: 3}}},
		&controlv1.BackendMessage{Sequence: 3, Body: &controlv1.BackendMessage_LedgerAck{LedgerAck: &controlv1.LedgerAcknowledgement{RecordId: "node/batch", Kind: "known", LedgerSequence: 10, PayloadSha256: make([]byte, 32)}}},
		&controlv1.HubMessage{Sequence: 1, Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: "node/batch", Kind: "known", LedgerSequence: 10, Payload: []byte{0, 1, 2}, PayloadSha256: make([]byte, 32)}}},
	}
	for _, message := range messages {
		name := string(message.ProtoReflect().Descriptor().FullName())
		t.Run(name, func(t *testing.T) {
			data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			decoded := message.ProtoReflect().New().Interface()
			if err := proto.Unmarshal(data, decoded); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(message, decoded) {
				t.Fatalf("round trip changed %s", name)
			}
		})
	}
}
