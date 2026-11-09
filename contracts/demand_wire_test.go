package contracts

import (
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"testing"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestDemandWireTagsPreservePublishedLifecycle(t *testing.T) {
	for _, tc := range []struct {
		m      proto.Message
		fields map[protoreflect.Name]protoreflect.FieldNumber
	}{
		{&controlv1.NodeMessage{}, map[protoreflect.Name]protoreflect.FieldNumber{"heartbeat": 10, "usage_batch": 11, "apply_result": 12, "event": 13, "register": 14, "unknown_usage_batch": 15, "lifecycle_hello": 16, "node_report": 17, "renewal_request": 18, "credit_demand": 100}},
		{&controlv1.ControlMessage{}, map[protoreflect.Name]protoreflect.FieldNumber{"config_update": 10, "usage_ack": 11, "authorization_update": 12, "error": 13, "unknown_usage_ack": 14, "hub_status": 15, "renewal_result": 16, "credential_revocations": 17, "credit_demand_denied": 100}},
	} {
		fields := tc.m.ProtoReflect().Descriptor().Fields()
		for name, tag := range tc.fields {
			if f := fields.ByName(name); f == nil || f.Number() != tag {
				t.Fatalf("%T.%s changed published field %d", tc.m, name, tag)
			}
		}
	}
	// Old experimental tags must never alias the migrated demand oneofs. They
	// decode as lifecycle messages and require that independent negotiated path.
	demand, _ := proto.Marshal(&controlv1.CreditDemand{RequestId: "request", DelegationId: "delegation", RawBytes: 1, TtlSeconds: 1})
	raw := protowire.AppendBytes(protowire.AppendTag(nil, 16, protowire.BytesType), demand)
	var node controlv1.NodeMessage
	if err := proto.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	if node.GetCreditDemand() != nil || node.GetLifecycleHello() == nil {
		t.Fatal("old demand aliased migrated demand")
	}
	denial, _ := proto.Marshal(&controlv1.CreditDemandDenied{RequestId: "request", Reason: controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_EXPIRED})
	raw = protowire.AppendBytes(protowire.AppendTag(nil, 15, protowire.BytesType), denial)
	var control controlv1.ControlMessage
	if err := proto.Unmarshal(raw, &control); err != nil {
		t.Fatal(err)
	}
	if control.GetCreditDemandDenied() != nil || control.GetHubStatus() == nil {
		t.Fatal("old denial aliased migrated denial")
	}
}
