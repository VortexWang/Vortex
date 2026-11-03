package vlink

import (
	"context"
	"errors"
	"testing"
)

func TestOpenPolicyUsesGenerationAdmissionBeforeNextHop(t *testing.T) {
	snapshot := validRuleSnapshot()
	rejected := errors.New("path: generation is draining")
	policy := OpenPolicy{
		LocalNode:                NodeID{1},
		Lookup:                   func(context.Context, [16]byte) (RuleSnapshot, bool) { return snapshot, true },
		SessionBillingSnapshotID: "bill-1", SessionBillingCoefficient: 7,
		GenerationAdmission: func(context.Context, OpenRequest) error { return rejected },
	}
	_, err := policy.Authorize(context.Background(), NodeID{2}, validOpenRequest())
	var rejection *OpenRejection
	if !errors.As(err, &rejection) || rejection.Code != OpenStaleRevision {
		t.Fatalf("err=%v", err)
	}
}

func TestBillingSnapshotRejectsPathCoefficientReuse(t *testing.T) {
	snapshot := validRuleSnapshot()
	policy := OpenPolicy{LocalNode: NodeID{1}, Lookup: func(context.Context, [16]byte) (RuleSnapshot, bool) { return snapshot, true }, SessionBillingSnapshotID: "bill-1", SessionBillingCoefficient: 8}
	_, err := policy.Authorize(context.Background(), NodeID{2}, validOpenRequest())
	var rejection *OpenRejection
	if !errors.As(err, &rejection) || rejection.Code != OpenQuotaExhausted {
		t.Fatalf("err=%v", err)
	}
}
