package contracts

import (
	"bytes"
	"crypto/sha256"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func validRenewal() *controlv1.RenewalRequest {
	return &controlv1.RenewalRequest{RequestId: "550e8400-e29b-41d4-a716-446655440001", NodeId: "550e8400-e29b-41d4-a716-446655440002", BootId: "550e8400-e29b-41d4-a716-446655440003", DelegationId: "550e8400-e29b-41d4-a716-446655440004", SourceId: "550e8400-e29b-41d4-a716-446655440005", BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440006", RuleId: "rule", RuleRevision: 1, PathGeneration: 2, FencingGeneration: 3, SignerEpoch: 4, RequestedRawBytes: 128, RequestedTtlSeconds: 60}
}

func TestRenewalDigestAndStrictBounds(t *testing.T) {
	r := validRenewal()
	raw, _ := (proto.MarshalOptions{Deterministic: true}).Marshal(r)
	want := sha256.Sum256(append([]byte("vortex.renewal.v1\x00"), raw...))
	digest, err := RenewalDigest(r)
	if err != nil || !bytes.Equal(want[:], digest) {
		t.Fatalf("digest: %x %v", digest, err)
	}
	r.RequestSha256 = digest
	payload, err := EncodeRenewalRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRenewalRequest(payload)
	if err != nil || !proto.Equal(r, decoded) {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, change := range []func(*controlv1.RenewalRequest){
		func(r *controlv1.RenewalRequest) { r.NodeId = "NODE" },
		func(r *controlv1.RenewalRequest) { r.RequestedRawBytes = math.MaxUint64 },
		func(r *controlv1.RenewalRequest) { r.RequestedTtlSeconds = 301 },
		func(r *controlv1.RenewalRequest) { r.FencingGeneration = 0 },
		func(r *controlv1.RenewalRequest) { r.RequestSha256 = make([]byte, 32) },
		func(r *controlv1.RenewalRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		bad := proto.Clone(r).(*controlv1.RenewalRequest)
		change(bad)
		if _, err := EncodeRenewalRequest(bad); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}

func TestRenewalResultRejectsUncorrelatedOrInventedSuccess(t *testing.T) {
	r := validRenewal()
	r.RequestSha256, _ = RenewalDigest(r)
	result := &controlv1.RenewalResult{RequestId: r.RequestId, RequestSha256: r.RequestSha256, Code: "ok", AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: r.PathGeneration, Payload: []byte("opaque")}}
	if _, err := EncodeRenewalResult(r, result); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*controlv1.RenewalResult){
		func(r *controlv1.RenewalResult) { r.RequestId = "other" },
		func(r *controlv1.RenewalResult) { r.RequestSha256 = make([]byte, 32) },
		func(r *controlv1.RenewalResult) { r.Retryable = true },
		func(r *controlv1.RenewalResult) { r.AuthorizationUpdate.Generation++ },
		func(r *controlv1.RenewalResult) { r.Code = "quota_exhausted" },
		func(r *controlv1.RenewalResult) { r.AuthorizationUpdate = nil },
	} {
		bad := proto.Clone(result).(*controlv1.RenewalResult)
		change(bad)
		if _, err := EncodeRenewalResult(r, bad); err == nil {
			t.Fatal("bad result accepted")
		}
	}
}
