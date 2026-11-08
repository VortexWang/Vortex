package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"math"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
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
		if delta == nil || delta.RuleId != snapshot.RuleID || delta.Bytes > math.MaxInt64 || len(delta.ProtoReflect().GetUnknown()) != 0 || (delta.Direction != "ingress" && delta.Direction != "egress") {
			return empty, ErrConflict
		}
		// Actual Engine uses source for the first platform execution point. A
		// reservation must bind that node; a relay cannot borrow the source grant.
		role := delta.Role
		if role == "source" {
			role = "execution"
			if snapshot.MultiplierMilli == 0 && delta.GrantId == "" {
				role = "self_hosted"
			}
		}
		if role != "execution" && role != "relay" && role != "exit" && role != "self_hosted" {
			return empty, ErrConflict
		}
		if role == "execution" && delta.GrantId == "" {
			return empty, ErrConflict
		}
		if role == "self_hosted" && (snapshot.MultiplierMilli != 0 || delta.GrantId != "") {
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

func (s MeteringService) IngestWireUnknown(ctx context.Context, node, reportBoot string, sequence uint64, b *controlv1.UnknownUsageBatch) (UsageAck, error) {
	var ack UsageAck
	if b == nil || sequence == 0 || sequence > math.MaxInt64 || b.UnknownUpperBound > math.MaxInt64 || len(b.ProtoReflect().GetUnknown()) != 0 || b.Role != "source" || (b.Direction != "ingress" && b.Direction != "egress") {
		return ack, ErrConflict
	}
	for _, id := range []string{node, reportBoot, b.OriginBootId} {
		if _, e := uuid.Parse(id); e != nil {
			return ack, e
		}
	}
	snapshot, e := generated.New(s.Pool).WireBillingSnapshot(ctx, b.BillingSnapshotId)
	if e != nil {
		return ack, e
	}
	scope, e := NewTenantScope(uuid.NewSHA1(uuid.NameSpaceOID, []byte("vortex/metering/"+node)).String(), snapshot.TenantID, snapshot.TenantID)
	if e != nil {
		return ack, e
	}
	payload, e := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if e != nil {
		return ack, e
	}
	digest := sha256.Sum256(payload)
	conflict := false
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "metering.ingest_unknown", ResourceID: b.GrantId, RequestID: uuid.NewString(), Reason: "authenticated boot restart upper bound; preserve original grant and frozen liability"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, node+"/"+reportBoot); e != nil {
			return e
		}
		r, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: b.GrantId})
		if e != nil {
			return e
		}
		if r.NodeID != node || r.BootID != b.OriginBootId || r.BillingSnapshotID != b.BillingSnapshotId || snapshot.RuleID != b.RuleId {
			return ErrUsageDisputed
		}
		old, e := q.GetUsageTuple(ctx, generated.GetUsageTupleParams{NodeID: node, BootID: reportBoot, Sequence: int64(sequence)})
		if errors.Is(e, pgx.ErrNoRows) {
			old, e = q.GetUsageBatchID(ctx, generated.GetUsageBatchIDParams{NodeID: node, BootID: reportBoot, BatchID: b.BatchId})
		}
		if e == nil {
			if old.TenantID == scope.TenantID() && old.BillingSnapshotID == b.BillingSnapshotId && bytes.Equal(old.PayloadSha256, digest[:]) {
				return json.Unmarshal(old.Acknowledgement, &ack)
			}
			conflict = true
			return dispute(ctx, q, scope, r, "unknown tuple conflict", map[string]any{"digest": digest[:], "batch_id": b.BatchId})
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = recordUnknown(ctx, q, scope, b.GrantId, digest[:], time.Now()); e != nil {
			return e
		}
		u, e := q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: r.ID})
		if e != nil {
			return e
		}
		// Never trust a node's smaller claimed upper bound to release occupancy.
		if int64(b.UnknownUpperBound) > u.UnknownRaw {
			return ErrConflict
		}
		ack = UsageAck{BatchID: b.BatchId, Digest: digest[:], State: "unknown_frozen"}
		return q.AppendUsageBatch(ctx, generated.AppendUsageBatchParams{TenantID: scope.TenantID(), NodeID: node, BootID: reportBoot, Sequence: int64(sequence), BatchID: b.BatchId, BillingSnapshotID: b.BillingSnapshotId, PayloadSha256: digest[:], Payload: payload, RawCumulative: numerator(u.ConfirmedRaw, 1), BillableNumerator: numerator(u.ConfirmedRaw, r.CoefficientMilli), UnknownUpperBound: numerator(u.UnknownRaw, 1), Acknowledgement: auditJSON(ack), Facts: auditJSON([]UsageFact{{GrantID: r.ID, RawCumulative: u.ConfirmedRaw, Role: "execution"}})})
	})
	if e == nil && conflict {
		e = ErrUsageDisputed
	}
	return ack, e
}
