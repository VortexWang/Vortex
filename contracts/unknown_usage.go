package contracts

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

var canonicalUsageUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// EncodeUnknownUsage enforces the approved 1.2.0 liability message contract.
// No bytes here represent proven consumption or permission to release funds.
func EncodeUnknownUsage(batch *controlv1.UnknownUsageBatch) ([]byte, error) {
	if batch == nil || len(batch.GetBatchId()) == 0 || len(batch.GetBatchId()) > 128 || !utf8.ValidString(batch.GetBatchId()) || len(batch.GetRuleId()) == 0 || len(batch.GetRuleId()) > 128 || !utf8.ValidString(batch.GetRuleId()) {
		return nil, fmt.Errorf("unknown usage: invalid batch or rule identity")
	}
	for _, id := range []string{batch.GetOriginBootId(), batch.GetBillingSnapshotId(), batch.GetGrantId()} {
		if !canonicalUsageUUID.MatchString(id) {
			return nil, fmt.Errorf("unknown usage: noncanonical UUID")
		}
	}
	if batch.GetRole() != "source" || (batch.GetDirection() != "ingress" && batch.GetDirection() != "egress") || batch.GetUnknownUpperBound() == 0 || batch.GetUnknownUpperBound() > math.MaxInt64 || len(batch.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("unknown usage: invalid role, direction or liability bound")
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(batch)
}

func DecodeUnknownUsage(payload []byte) (*controlv1.UnknownUsageBatch, error) {
	if len(payload) == 0 || len(payload) > 1024 {
		return nil, fmt.Errorf("unknown usage: invalid payload size")
	}
	batch := new(controlv1.UnknownUsageBatch)
	if err := proto.Unmarshal(payload, batch); err != nil {
		return nil, err
	}
	canonical, err := EncodeUnknownUsage(batch)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, payload) {
		return nil, fmt.Errorf("unknown usage: nondeterministic payload encoding")
	}
	return batch, nil
}
