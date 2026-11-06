package contracts

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// RenewalDigest implements the approved lifecycle digest. It does not authorize
// a request; the issuer must separately check its durable allocation and scope.
func RenewalDigest(request *controlv1.RenewalRequest) ([]byte, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("renewal: missing request or unknown fields")
	}
	for _, id := range []string{request.RequestId, request.NodeId, request.BootId, request.DelegationId, request.SourceId, request.BillingSnapshotId} {
		if !canonicalUsageUUID.MatchString(id) {
			return nil, fmt.Errorf("renewal: noncanonical UUID")
		}
	}
	if request.RuleId == "" || len(request.RuleId) > 128 || !utf8.ValidString(request.RuleId) {
		return nil, fmt.Errorf("renewal: invalid rule")
	}
	for _, value := range []uint64{request.RuleRevision, request.PathGeneration, request.FencingGeneration, request.SignerEpoch, request.RequestedRawBytes} {
		if value == 0 || value > math.MaxInt64 {
			return nil, fmt.Errorf("renewal: unsafe counter or amount")
		}
	}
	if request.RequestedTtlSeconds == 0 || request.RequestedTtlSeconds > 300 {
		return nil, fmt.Errorf("renewal: invalid TTL")
	}
	copy := proto.Clone(request).(*controlv1.RenewalRequest)
	copy.RequestSha256 = nil
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(copy)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append([]byte("vortex.renewal.v1\x00"), payload...))
	return sum[:], nil
}

// EncodeRenewalRequest checks an already sealed request without mutating it.
func EncodeRenewalRequest(request *controlv1.RenewalRequest) ([]byte, error) {
	digest, err := RenewalDigest(request)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(request.RequestSha256, digest) {
		return nil, fmt.Errorf("renewal: request digest mismatch")
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(request)
}

func DecodeRenewalRequest(payload []byte) (*controlv1.RenewalRequest, error) {
	if len(payload) == 0 || len(payload) > 4096 {
		return nil, fmt.Errorf("renewal: invalid request size")
	}
	request := new(controlv1.RenewalRequest)
	if err := proto.Unmarshal(payload, request); err != nil {
		return nil, err
	}
	canonical, err := EncodeRenewalRequest(request)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, payload) {
		return nil, fmt.Errorf("renewal: noncanonical request encoding")
	}
	return request, nil
}

// EncodeRenewalResult checks transport shape and exact request correlation.
// Success payloads still require the full authorization validator before use.
func EncodeRenewalResult(request *controlv1.RenewalRequest, result *controlv1.RenewalResult) ([]byte, error) {
	if _, err := EncodeRenewalRequest(request); err != nil {
		return nil, err
	}
	if result == nil || len(result.ProtoReflect().GetUnknown()) != 0 || result.RequestId != request.RequestId || !bytes.Equal(result.RequestSha256, request.RequestSha256) {
		return nil, fmt.Errorf("renewal: uncorrelated result")
	}
	if result.Code == "ok" {
		u := result.AuthorizationUpdate
		if result.Retryable || u == nil || len(u.ProtoReflect().GetUnknown()) != 0 || u.Generation != request.PathGeneration || len(u.Payload) == 0 || len(u.Payload) > (1<<20)-4096 {
			return nil, fmt.Errorf("renewal: invalid success result")
		}
	} else {
		if result.AuthorizationUpdate != nil {
			return nil, fmt.Errorf("renewal: rejection includes authority")
		}
		switch result.Code {
		case "storage_unavailable", "temporarily_unavailable":
		case "invalid_request", "unsupported_capability", "identity_mismatch", "stale_session", "stale_revision", "closed_epoch", "scope_denied", "quota_exhausted", "time_uncertain":
			if result.Retryable {
				return nil, fmt.Errorf("renewal: permanent rejection marked retryable")
			}
		default:
			return nil, fmt.Errorf("renewal: unknown result code")
		}
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(result)
}
