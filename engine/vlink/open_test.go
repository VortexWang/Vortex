package vlink

import (
	"context"
	"errors"
	"testing"
)

func TestOpenPolicyAcceptsLocalRuntimeDecision(t *testing.T) {
	peer := NodeID{2}
	snapshot := validRuleSnapshot()
	audits := make(chan OpenAuditEvent, 4)
	policy := OpenPolicy{
		LocalNode: NodeID{1},
		Lookup: func(_ context.Context, id [16]byte) (RuleSnapshot, bool) {
			if id != snapshot.RuleID {
				t.Fatal("lookup did not use requested rule")
			}
			return snapshot, true
		},
		Audit:                    func(event OpenAuditEvent) { audits <- event },
		SessionBillingSnapshotID: "bill-1", SessionBillingCoefficient: 7,
	}
	decision, err := policy.Authorize(context.Background(), peer, validOpenRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.NextHop != (NodeID{3}) {
		t.Fatalf("next hop=%v", decision.NextHop)
	}
	if len(audits) != 0 {
		t.Fatal("audited an accepted OPEN as rejection")
	}
}

func TestOpenPolicyStableRejectionClassesAndAuditCorrelation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuleSnapshot, *OpenRequest, *OpenPolicy)
		found  bool
		want   OpenErrorCode
	}{
		{name: "missing local snapshot", found: false, want: OpenNoRoute},
		{name: "stale revision", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.RuleRevision++ }, want: OpenStaleRevision},
		{name: "stale path", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.PathGeneration++ }, want: OpenStaleRevision},
		{name: "invalid hop", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.HopIndex++ }, want: OpenNoRoute},
		{name: "untrusted peer", mutate: func(s *RuleSnapshot, _ *OpenRequest, _ *OpenPolicy) { delete(s.AllowedPeers, NodeID{2}) }, want: OpenUnauthorized},
		{name: "own node visited", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.Visited = append(r.Visited, NodeID{1}) }, want: OpenNoRoute},
		{name: "no eligible next hop", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) {
			r.Visited = append(r.Visited, NodeID{3}, NodeID{4})
		}, want: OpenNoRoute},
		{name: "runtime authorization", mutate: func(s *RuleSnapshot, _ *OpenRequest, _ *OpenPolicy) { s.RuntimeAuthorized = false }, want: OpenUnauthorized},
		{name: "rate share", mutate: func(s *RuleSnapshot, _ *OpenRequest, _ *OpenPolicy) { s.RateShareValid = false }, want: OpenRateLimited},
		{name: "connection share", mutate: func(s *RuleSnapshot, _ *OpenRequest, _ *OpenPolicy) { s.ConnectionShareValid = false }, want: OpenConnectionLimited},
		{name: "billing snapshot", mutate: func(_ *RuleSnapshot, _ *OpenRequest, p *OpenPolicy) { p.SessionBillingSnapshotID = "other" }, want: OpenQuotaExhausted},
		{name: "billing authorization", mutate: func(s *RuleSnapshot, _ *OpenRequest, _ *OpenPolicy) { s.BillingAuthorized = false }, want: OpenQuotaExhausted},
		{name: "network not configured", mutate: func(s *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { s.AllowedNetworks[r.Network] = false }, want: OpenUnauthorized},
		{name: "invalid network", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.Network = 9 }, want: OpenProtocolError},
		{name: "invalid original client", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.ClientAddress = []byte{0, 0, 0, 0} }, want: OpenProtocolError},
		{name: "duplicate visited node", mutate: func(_ *RuleSnapshot, r *OpenRequest, _ *OpenPolicy) { r.Visited = []NodeID{{5}, {5}} }, want: OpenProtocolError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, request := validRuleSnapshot(), validOpenRequest()
			policy := OpenPolicy{LocalNode: NodeID{1}, Lookup: func(context.Context, [16]byte) (RuleSnapshot, bool) {
				return snapshot, test.found || test.name != "missing local snapshot"
			}, SessionBillingSnapshotID: "bill-1", SessionBillingCoefficient: 7}
			policy.Audit = func(event OpenAuditEvent) {
				if event.PeerID != (NodeID{2}) || event.RuleID != request.RuleID {
					t.Errorf("unexpected audit event: %+v", event)
				}
			}
			if test.mutate != nil {
				test.mutate(&snapshot, &request, &policy)
			}
			_, err := policy.Authorize(context.Background(), NodeID{2}, request)
			var rejection *OpenRejection
			if !errors.As(err, &rejection) || rejection.Code != test.want {
				t.Fatalf("rejection=%v want=%d", err, test.want)
			}
			payload, err := OpenErrorPayload(rejection)
			if err != nil || len(payload) != 18 || OpenErrorCode(payload[0])<<8|OpenErrorCode(payload[1]) != test.want {
				t.Fatalf("wire rejection=%v payload=%x", err, payload)
			}
			if rejection.CorrelationID == ([16]byte{}) {
				t.Fatal("missing correlation ID")
			}
		})
	}
}

func TestOpenPolicySelfHostedAndNonExecutionRulesUseCorrectGates(t *testing.T) {
	snapshots := []RuleSnapshot{
		func() RuleSnapshot {
			s := validRuleSnapshot()
			s.SelfHosted = true
			s.RateShareValid = false
			s.ConnectionShareValid = false
			s.BillingSnapshotID = ""
			return s
		}(),
		func() RuleSnapshot {
			s := validRuleSnapshot()
			s.ExecutionPoint = false
			s.RateShareValid = false
			s.ConnectionShareValid = false
			s.BillingSnapshotID = ""
			return s
		}(),
	}
	for _, snapshot := range snapshots {
		policy := OpenPolicy{LocalNode: NodeID{1}, Lookup: func(context.Context, [16]byte) (RuleSnapshot, bool) { return snapshot, true }}
		if _, err := policy.Authorize(context.Background(), NodeID{2}, validOpenRequest()); err != nil {
			t.Fatal(err)
		}
	}
}

func validRuleSnapshot() RuleSnapshot {
	return RuleSnapshot{RuleID: [16]byte{9}, Revision: 3, PathGeneration: 4, HopIndex: 1, ExecutionPoint: true, Active: true, AllowedPeers: map[NodeID]bool{{2}: true}, AllowedNetworks: map[uint8]bool{1: true, 2: true}, NextHops: []NodeID{{3}, {4}}, RuntimeAuthorized: true, SegmentAllowed: true, RateShareValid: true, ConnectionShareValid: true, BillingAuthorized: true, BillingSnapshotID: "bill-1", BillingCoefficient: 7}
}

func validOpenRequest() OpenRequest {
	return OpenRequest{RuleID: [16]byte{9}, RuleRevision: 3, PathGeneration: 4, HopIndex: 1, Network: 1, ClientAddress: []byte{203, 0, 113, 8}}
}
