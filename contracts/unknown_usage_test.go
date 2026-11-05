package contracts

import (
	"bytes"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func validUnknown() *controlv1.UnknownUsageBatch {
	return &controlv1.UnknownUsageBatch{BatchId: "batch", OriginBootId: "550e8400-e29b-41d4-a716-446655440001", BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440002", GrantId: "550e8400-e29b-41d4-a716-446655440003", RuleId: "rule", Role: "source", Direction: "egress", UnknownUpperBound: 1024}
}

func TestUnknownUsageBoundsAndDeterministicEncoding(t *testing.T) {
	b := validUnknown()
	x, err := EncodeUnknownUsage(b)
	if err != nil {
		t.Fatal(err)
	}
	y, err := EncodeUnknownUsage(b)
	if err != nil || !bytes.Equal(x, y) {
		t.Fatal("unstable bytes")
	}
	if decoded, err := DecodeUnknownUsage(x); err != nil || !proto.Equal(decoded, b) {
		t.Fatal(err)
	}
	for _, change := range []func(*controlv1.UnknownUsageBatch){
		func(b *controlv1.UnknownUsageBatch) { b.BatchId = "" },
		func(b *controlv1.UnknownUsageBatch) { b.RuleId = string(bytes.Repeat([]byte{'x'}, 129)) },
		func(b *controlv1.UnknownUsageBatch) { b.GrantId = "not-a-uuid" },
		func(b *controlv1.UnknownUsageBatch) { b.OriginBootId = "" },
		func(b *controlv1.UnknownUsageBatch) { b.BillingSnapshotId = "UPPER-CASE" },
		func(b *controlv1.UnknownUsageBatch) { b.Direction = "both" },
		func(b *controlv1.UnknownUsageBatch) { b.Role = "destination" },
		func(b *controlv1.UnknownUsageBatch) { b.UnknownUpperBound = 0 },
		func(b *controlv1.UnknownUsageBatch) { b.UnknownUpperBound = math.MaxUint64 },
	} {
		b := validUnknown()
		change(b)
		if _, err := EncodeUnknownUsage(b); err == nil {
			t.Fatalf("accepted invalid unknown: %s", b)
		}
	}
	// Duplicate field encoding and unknown fields cannot yield a second raw
	// digest for the same logical accepted payload.
	if _, err := DecodeUnknownUsage(append(x, 0x0a, 0x05, 'b', 'a', 't', 'c', 'h')); err == nil {
		t.Fatal("duplicate encoding accepted")
	}
	if _, err := DecodeUnknownUsage(append(x, 0xa0, 0x06, 0x01)); err == nil {
		t.Fatal("unrecognized field accepted")
	}
}
