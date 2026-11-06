package contracts

import (
	"fmt"
	"google.golang.org/protobuf/proto"
	"math"
	"regexp"
	"time"
	controlv1 "vortex/contracts/protobuf/v1"
)

var canonicalCertificateSerial = regexp.MustCompile(`^[1-9][0-9]{0,48}$`)

// EncodeCredentialRevocations validates the already approved lifecycle body.
// Authentication and trust-domain scope must be checked by the receiver.
func EncodeCredentialRevocations(r *controlv1.CredentialRevocations) ([]byte, error) {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || proto.Size(r) > 1<<20 || !canonicalUsageUUID.MatchString(r.IssuerId) || r.Revision == 0 || r.Revision > math.MaxInt64 || len(r.CertificateSerials) > 4096 || len(r.NodeIds) > 4096 || len(r.EffectiveAt) > 35 {
		return nil, fmt.Errorf("credential revocations: invalid envelope")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.EffectiveAt); err != nil {
		return nil, err
	}
	serials, nodes := map[string]bool{}, map[string]bool{}
	for _, v := range r.CertificateSerials {
		if !canonicalCertificateSerial.MatchString(v) || serials[v] {
			return nil, fmt.Errorf("credential revocations: invalid/duplicate serial")
		}
		serials[v] = true
	}
	for _, v := range r.NodeIds {
		if !canonicalUsageUUID.MatchString(v) || nodes[v] {
			return nil, fmt.Errorf("credential revocations: invalid/duplicate node")
		}
		nodes[v] = true
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(r)
}
