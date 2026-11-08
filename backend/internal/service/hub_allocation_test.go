package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"path/filepath"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func fundedHubFixture(t *testing.T) (*shareFixture, service.HubFundingInput, *store.Store) {
	return fundedHubFixtureMultiplier(t, 1000)
}
func fundedHubFixtureMultiplier(t *testing.T, multiplier int64) (*shareFixture, service.HubFundingInput, *store.Store) {
	t.Helper()
	x := newShareFixtureTermsMultiplier(t, packageTerms(), multiplier)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	sources, e := q.ListTrafficSources(ctx, x.f.tenant)
	if e != nil || len(sources) != 1 {
		t.Fatal(e, sources)
	}
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	// Use the Node's already persisted Hub assignment without changing the
	// immutable prepared port/member provenance. This adapter fixture signs a
	// Hub certificate with its test CA; production has a separate Hub root.
	hubID, e := q.UsageNodeHub(ctx, x.in.NodeID)
	if e != nil {
		t.Fatal(e)
	}
	x.in.SignerID = hubID
	x.f.contexts[hubID] = portNodeContext(t, x.s.SignerCA, x.f.caKey, hubID)
	proof := sha256.Sum256([]byte("explicit funded fixture Hub epoch"))
	if _, e = x.s.AuthorizeSigner(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), hubID, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	node, e := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { node.Close() })
	original := x.in
	for _, kind := range []string{"rate", "connections"} {
		x.in.Kind = kind
		x.in.Nonce = uuid.NewString()
		if kind == "connections" {
			x.in.Amount = 4
			x.in.BurstBytes = 0
		}
		p := x.prepare(t)
		if e = x.s.QueueHandoff(ctx, x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		confirmHandoff(t, x, node, p.Generation.ID)
	}
	x.in = original
	input := service.HubFundingInput{NodeID: x.in.NodeID, BootID: x.in.BootID, HubID: x.in.SignerID, HubBootID: uuid.NewString(), Region: "phase6", SourceID: sources[0].ID, SnapshotID: b.ID, Nonce: uuid.NewString(), SignerEpoch: 7, PathGeneration: 1, FencingFloor: 3, RawBytes: 103, ExpiresAt: x.now.Add(10 * time.Minute)}
	if _, e = x.s.BindHubSession(x.f.contexts[hubID], hubID, input.HubBootID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	hs, e := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { hs.Close() })
	return x, input, hs
}

func installAllocationFixture(t *testing.T, hs *store.Store, a hub.Allocation, now time.Time) {
	t.Helper()
	ctx := context.Background()
	raw, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := hub.DecodeAllocation(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = hs.InstallFundedHubDelegation(ctx, shareBackendID, decoded.Delegation(), store.IssuerTime{Earliest: now, Latest: now, Trusted: true}); e != nil {
		t.Fatal(e)
	}
}
func issueAllocationFixture(t *testing.T, x *shareFixture, hs *store.Store, a hub.Allocation, n uint64) (store.IssuedAuthorization, string, int64) {
	t.Helper()
	ctx := context.Background()
	r, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: a.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: n, TTL: 300 * time.Second}, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	facts, e := agent.InspectAuthorizationUpdate(r.Payload, a.NodeID, a.BootID, a.PathGeneration)
	if e != nil {
		t.Fatal(e)
	}
	grant := ""
	for _, v := range facts.Records {
		if v.Kind == "credit_grant" {
			grant = v.AuthorizationID
		}
	}
	records, e := hs.PendingLedger(ctx, 1000)
	if e != nil {
		t.Fatal(e)
	}
	for _, rec := range records {
		if bytes.Equal(rec.Payload, r.Payload) {
			return r, grant, rec.Sequence
		}
	}
	t.Fatal("actual FULL issuer journal missing")
	return r, "", 0
}

func TestHubFundingActualIssuerChildrenOneParentAndAuditRollback(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"reservations", "ledger_events", "ledger_projections", "hub_funded_allocations", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, request); e == nil {
		t.Fatal("funding escaped failed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial funds/allocation")
	}
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	replay, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil || !jsonEqualTest(a, replay) {
		t.Fatal("allocation replay refilled", e)
	}
	changed := in
	changed.RawBytes++
	if _, e = x.s.PrepareHubAllocation(ctx, x.f.scope, changed, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("nonce rebound", e)
	}
	installAllocationFixture(t, hs, a, x.now.Add(time.Second))
	first, grant1, seq1 := issueAllocationFixture(t, x, hs, a, 51)
	if e = x.s.RecordHubIssue(ctx, a.HubID, in.HubBootID, seq1, first.Payload, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned issue accepted", e)
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	childTables := []string{"hub_issue_receipts", "hub_credit_grants", "ledger_events", "ledger_projections", "tasks", "outbox", "operations", "audit_events"}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, childTables)
	if e = x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, in.HubBootID, seq1, first.Payload, request); e == nil {
		t.Fatal("issue escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, childTables)) {
		t.Fatal("partial child mapping")
	}
	for i := 0; i < 2; i++ {
		if e = x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, in.HubBootID, seq1, first.Payload, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	second, grant2, seq2 := issueAllocationFixture(t, x, hs, a, 52)
	var forged map[string]any
	if e = json.Unmarshal(second.Payload, &forged); e != nil {
		t.Fatal(e)
	}
	for _, entry := range forged["authorizations"].([]any) {
		r := entry.(map[string]any)
		if r["kind"] == "credit_grant" {
			r["amount_bytes"] = float64(53)
		}
	}
	over, e := json.Marshal(forged)
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, in.HubBootID, seq2, over, uuid.NewString()); !errors.Is(e, service.ErrShareCapacity) {
		t.Fatal("Hub exceeded funded parent", e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, in.HubBootID, seq2, second.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	record, e := q.HubAllocationByDelegation(ctx, a.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: x.f.tenant, ID: record.ReservationID})
	if e != nil {
		t.Fatal(e)
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != parent.ReservedBill || p.Charged != 0 {
		t.Fatal("children double funded parent", e, p, parent)
	}
	// Consume real Hub-generated credit IDs through the actual Agent wire.
	metering := service.MeteringService{Pool: x.f.pool}
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: a.SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: a.RuleID, GrantId: grant1, Role: "source", Direction: "ingress", Bytes: 51}, {RuleId: a.RuleID, GrantId: grant2, Role: "source", Direction: "egress", Bytes: 50}}}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	payload, e := json.Marshal(batch)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(payload)
	input := service.UsageInput{NodeID: a.NodeID, BootID: a.BootID, SnapshotID: a.SnapshotID, BatchID: batch.BatchId, Sequence: 1, Payload: payload, Digest: sum[:], Delta: true, Facts: []service.UsageFact{{GrantID: grant1, RawCumulative: 51, Role: "execution"}, {GrantID: grant2, RawCumulative: 50, Role: "execution"}}}
	usageTables := []string{"hub_credit_usage", "settlements", "usage_batches", "ledger_events", "ledger_projections", "audit_events", "tasks", "operations", "outbox"}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, usageTables)
	if _, e = metering.IngestUsageBatch(ctx, x.f.scope, input, request); e == nil {
		t.Fatal("child usage escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, usageTables)) {
		t.Fatal("partial child usage")
	}
	for i := 0; i < 2; i++ {
		if _, e = metering.IngestWireUsage(ctx, a.NodeID, a.BootID, 1, batch); e != nil {
			t.Fatal(e)
		}
	}
	accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: parent.ID})
	if e != nil || accounting.RawCumulative != 101 {
		t.Fatal("parent aggregate replay", e, accounting)
	}
	p, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.Charged != 101 || p.F != 2 {
		t.Fatal("source-local parent conservation", e, p)
	}
	// A child is not allowed to consume sibling capacity; neither a forged parent
	// ID nor a short final/return proof can return the unconfirmed remainder.
	bad := *batch
	bad.BatchId = uuid.NewString()
	bad.Deltas = []*controlv1.UsageDelta{{RuleId: a.RuleID, GrantId: grant1, Role: "source", Direction: "ingress", Bytes: 1}}
	if _, e = metering.IngestWireUsage(ctx, a.NodeID, a.BootID, 2, &bad); !errors.Is(e, service.ErrUsageDisputed) {
		t.Fatal("child exceeded its own partition", e)
	}
	proof := sha256.Sum256([]byte("incomplete child final must not release parent"))
	if e = metering.RecordFinalReceipt(ctx, x.f.scope, parent.ID, a.SnapshotID, "premature", 101, proof[:], uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("incomplete parent released", e)
	}
	unknown := &controlv1.UnknownUsageBatch{BatchId: uuid.NewString(), OriginBootId: a.BootID, BillingSnapshotId: a.SnapshotID, RuleId: a.RuleID, GrantId: grant2, Role: "source", Direction: "egress", UnknownUpperBound: 2}
	reportBoot := uuid.NewString()
	for i := 0; i < 2; i++ {
		if _, e = metering.IngestWireUnknown(ctx, a.NodeID, reportBoot, 1, unknown); e != nil {
			t.Fatal("child unknown wire", e)
		}
	}
	if e = metering.RecordUnknown(ctx, x.f.scope, parent.ID, proof[:], x.now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != 2 || p.L != 0 {
		t.Fatal("unknown child bound not retained", e, p)
	}
}
func jsonEqualTest(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestHubFundingFractionalChildrenDoNotCeilOrReserveTwice(t *testing.T) {
	x, in, hs := fundedHubFixtureMultiplier(t, 1)
	ctx := context.Background()
	in.RawBytes = 2
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now.Add(time.Second))
	deltas := []*controlv1.UsageDelta{}
	for i := 0; i < 2; i++ {
		result, grant, seq := issueAllocationFixture(t, x, hs, a, 1)
		if e = x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, in.HubBootID, seq, result.Payload, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		deltas = append(deltas, &controlv1.UsageDelta{RuleId: a.RuleID, GrantId: grant, Role: "source", Direction: "ingress", Bytes: 1})
	}
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: a.SnapshotID, Deltas: deltas}
	if _, e = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, a.NodeID, a.BootID, 1, batch); e != nil {
		t.Fatal(e)
	}
	q := generated.New(x.f.pool)
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != 1 || p.Charged != 0 || p.RemainderMilli != 2 {
		t.Fatal("per-child ceil charged or double reserved", e, p)
	}
}
