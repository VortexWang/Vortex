package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestUsageWireRejectsMalformedAndUnfundedSelfHosted(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	b, err := generated.New(f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"self_hosted", "nil"} {
		t.Run(role, func(t *testing.T) {
			var delta *controlv1.UsageDelta
			if role != "nil" {
				delta = &controlv1.UsageDelta{RuleId: b.RuleID, Role: role, Direction: "egress", Bytes: 123}
			}
			batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: snapshot, Deltas: []*controlv1.UsageDelta{delta}}
			if _, err := s.IngestWireUsage(ctx, f.nodes[0], uuid.NewString(), 1, batch); err == nil {
				t.Fatal("malformed or paid-snapshot self-hosted usage accepted")
			}
		})
	}
	p, err := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if err != nil || p.B != 10000 || p.F != 0 || p.Charged != 0 {
		t.Fatal("rejected wire changed source authority", err, p)
	}
}

func TestUnknownProtectionRejectsFutureStart(t *testing.T) {
	boundary := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	hi := boundary.Add(5 * time.Second)
	if state := service.ProtectionState(boundary.Add(-5*time.Second), hi, boundary, hi.Add(time.Second), boundary.Add(time.Hour), true); state != "close" {
		t.Fatalf("future protection start accepted: %s", state)
	}
}
