package service

import (
	"context"
	"crypto/sha256"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"math"
	"vortex/backend/db/generated"
	controlv1 "vortex/contracts/protobuf/v1"
)

// IngestWireUsage consumes the existing Agent wire, keeping byte deltas distinct
// from cumulative ledger facts. Caller must authenticate node through mTLS and
// pin boot to the connected stream. No synthetic defaults create financial rights.
func (s MeteringService) IngestWireUsage(ctx context.Context, node, boot string, sequence uint64, b *controlv1.UsageBatch) (UsageAck, error) {
	var empty UsageAck
	if b == nil || sequence == 0 || sequence > math.MaxInt64 || len(b.Deltas) == 0 || len(b.Deltas) > 1024 || len(b.ProtoReflect().GetUnknown()) != 0 {
		return empty, ErrConflict
	}
	snapshot, e := generated.New(s.Pool).WireBillingSnapshot(ctx, b.BillingSnapshotId)
	if e != nil {
		return empty, e
	}
	scope, e := NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/metering/"+node)).String(), snapshot.TenantID, snapshot.TenantID)
	if e != nil {
		return empty, e
	}
	payload, e := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if e != nil {
		return empty, e
	}
	digest := sha256.Sum256(payload)
	facts := map[string]UsageFact{}
	for _, delta := range b.Deltas {
		if delta.RuleId != snapshot.RuleID || delta.Bytes > math.MaxInt64 || delta.GrantId == "" || len(delta.ProtoReflect().GetUnknown()) != 0 || (delta.Direction != "ingress" && delta.Direction != "egress") {
			return empty, ErrConflict
		}
		// Actual Engine uses source for the first platform execution point. A
		// reservation must bind that node; a relay cannot borrow the source grant.
		role := delta.Role
		if role == "source" {
			role = "execution"
		}
		if role != "execution" && role != "relay" && role != "exit" {
			return empty, ErrConflict
		}
		f := facts[delta.GrantId]
		if f.GrantID != "" && f.Role != role {
			return empty, ErrConflict
		}
		if math.MaxInt64-f.RawCumulative < int64(delta.Bytes) {
			return empty, ErrConflict
		}
		f.GrantID = delta.GrantId
		f.Role = role
		f.RawCumulative += int64(delta.Bytes)
		facts[f.GrantID] = f
	}
	in := UsageInput{NodeID: node, BootID: boot, BatchID: b.BatchId, SnapshotID: b.BillingSnapshotId, Sequence: int64(sequence), Payload: payload, Digest: digest[:], Delta: true}
	for _, f := range facts {
		in.Facts = append(in.Facts, f)
	}
	return s.IngestUsageBatch(ctx, scope, in, uuid.NewString())
}
