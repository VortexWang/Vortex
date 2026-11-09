package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type creditCloseFixture struct {
	x      *shareFixture
	in     service.HubFundingInput
	a      hub.Allocation
	hs, st *store.Store
	node   *agent.Node
	issued store.IssuedAuthorization
	grant  string
}

func newCreditCloseFixture(t *testing.T) *creditCloseFixture {
	return newCreditCloseFixtureMultiplier(t, 1200, 51)
}

func newCreditCloseFixtureMultiplier(t *testing.T, multiplier int64, issuedBytes uint64) *creditCloseFixture {
	t.Helper()
	x, in, hs := fundedHubFixtureMultiplier(t, multiplier)
	ctx := context.Background()
	a, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, grant, seq := issueAllocationFixture(t, x, hs, a, issuedBytes)
	if err = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir()+"/node.db", "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	node, err := agent.New(in.NodeID, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.BootID = in.BootID
	return &creditCloseFixture{x: x, in: in, a: a, hs: hs, st: st, node: node, issued: issued, grant: grant}
}

func (f *creditCloseFixture) seal(t *testing.T) {
	t.Helper()
	seal, err := f.hs.SealHubDelegation(context.Background(), f.in.HubID, f.in.HubBootID, f.a.DelegationID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(seal)
	rows, err := f.hs.PendingLedger(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if bytes.Equal(row.Payload, raw) {
			if err = f.x.s.RecordDelegationSeal(f.x.f.contexts[f.in.HubID], f.in.HubID, f.in.HubBootID, row.Sequence, raw, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing FULL seal")
}

func (f *creditCloseFixture) localFinal(t *testing.T, p generated.NodeCreditCloseCommand) contracts.CreditCloseReceipt {
	t.Helper()
	final, err := f.node.CloseIssuedCreditGrant(context.Background(), f.in.HubID, &controlv1.AuthorizationUpdate{Generation: f.a.PathGeneration, Payload: f.issued.Payload}, f.grant)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(final)
	return contracts.CreditCloseReceipt{Version: 1, CommandID: p.CommandID, BackendID: p.BackendID, NodeID: p.NodeID, BootID: p.BootID, GrantID: p.GrantID, CommandSHA256: hex.EncodeToString(p.PayloadDigest), State: "final", Final: raw}
}

func TestCreditCloseSealGateAuditRollbackAndOriginalIdentity(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	ctx := context.Background()
	if _, err := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("close before complete seal", err)
	}
	f.seal(t)
	tables := []string{"node_credit_close_commands", "node_credit_close_receipts", "operations", "tasks", "outbox", "audit_events", "settlements", "ledger_projections"}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, err := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, request); err == nil {
		t.Fatal("command escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial command")
	}
	p, err := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	// Concurrent producers retain exact immutable commands and one operation.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r, e := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, uuid.NewString())
			if e != nil || r.CommandID != p.CommandID || !bytes.Equal(r.Payload, p.Payload) {
				t.Error("replay changed", e)
			}
		})
	}
	wg.Wait()
	if _, err = x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, uuid.NewString(), uuid.NewString()); !errors.Is(err, service.ErrShareConflict) {
		t.Fatal("backend rebound", err)
	}
	r := f.localFinal(t, p)
	raw, _ := json.Marshal(r)
	if err = x.s.RecordCreditClose(ctx, x.f.scope, raw, uuid.NewString()); !errors.Is(err, service.ErrScope) {
		t.Fatal("unsigned receipt", err)
	}
	for _, mutate := range []func(*contracts.CreditCloseReceipt){
		func(r *contracts.CreditCloseReceipt) { r.BootID = uuid.NewString() },
		func(r *contracts.CreditCloseReceipt) { r.CommandID = uuid.NewString() },
		func(r *contracts.CreditCloseReceipt) { r.CommandSHA256 = hex.EncodeToString(make([]byte, 32)) },
		func(r *contracts.CreditCloseReceipt) {
			var v store.CreditGrantFinal
			json.Unmarshal(r.Final, &v)
			v.Grant.AmountBytes++
			v.Unused++
			r.Final, _ = json.Marshal(v)
		},
		func(r *contracts.CreditCloseReceipt) {
			var v store.CreditGrantFinal
			json.Unmarshal(r.Final, &v)
			v.KnownUsage = contracts.KnownUsageInventory{}
			r.Final, _ = json.Marshal(v)
		},
	} {
		bad := r
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, b, uuid.NewString()); err == nil {
			t.Fatal("mutated receipt accepted", bad)
		}
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, raw, request); err == nil {
		t.Fatal("final escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial final")
	}
	for range 2 {
		if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, raw, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	q := generated.New(x.f.pool)
	op, err := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if err != nil || op.State != "succeeded" {
		t.Fatal(op, err)
	}
	projection, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || projection.F != 62 || projection.Charged != 0 {
		t.Fatal("child receipt released parent", projection, err)
	}
	for _, table := range []string{"node_credit_close_commands", "node_credit_close_receipts"} {
		if _, err = x.f.pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", x.f.tenant); err == nil {
			t.Fatal("mutable history", table)
		}
	}
}

// Install original credit in FULL SQLite, reserve/commit an actual wire batch,
// and retain the exact original bytes. Other runtime leases are outside this
// store adapter fixture (their actual Engine/TLS tests are separate).
func (f *creditCloseFixture) commitLocalUsage(t *testing.T, used uint64) *controlv1.UsageBatch {
	t.Helper()
	ctx := context.Background()
	for _, root := range f.a.ShareReferences {
		if err := f.st.InstallShareSourceAnchor(ctx, f.in.NodeID, shareBackendID, root); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := agent.InspectAuthorizationUpdate(f.issued.Payload, f.in.NodeID, f.in.BootID, f.a.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	r, err := agent.InspectIssuedCreditGrant(f.issued.Payload, f.in.NodeID, f.in.BootID, f.in.HubID, f.grant, f.a.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(f.issued.Payload)
	if err = f.st.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{ShareReferences: facts.ShareReferences, IssuerID: f.in.HubID, BackendHeartbeat: facts.BackendHeartbeat, AutonomyDeadline: facts.AutonomyDeadline, NodeID: f.in.NodeID, BootID: f.in.BootID, UpdateID: facts.UpdateID, Payload: f.issued.Payload, Digest: sum[:], SignerEpoch: facts.SignerEpoch, PathGeneration: facts.PathGeneration, FencingGenerationFloor: facts.FencingFloor}, []store.AuthorizationRecord{r}, nil); err != nil {
		t.Fatal(err)
	}
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: f.a.SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: f.a.RuleID, GrantId: f.grant, Role: "source", Direction: "egress", Bytes: used}}}
	when := facts.IssuedAt.Add(time.Millisecond)
	u := store.UsageReservation{AuthorizationID: f.grant, NodeID: f.in.NodeID, BootID: f.in.BootID, BatchID: batch.BatchId, SnapshotID: f.a.SnapshotID, FlowKey: "real-committed-batch", RuleID: f.a.RuleID, Direction: "egress", Reserved: min(used+1, r.AmountBytes)}
	_, err = f.st.ReserveAuthorizedUsage(ctx, u, store.AuthorizationUse{NodeID: f.in.NodeID, BootID: f.in.BootID, AuthorizationID: f.grant, Kind: "credit_grant", RuleID: f.a.RuleID, RuleRevision: f.a.RuleRevision, PathGeneration: f.a.PathGeneration, BillingSnapshotID: f.a.SnapshotID, Earliest: when, Latest: when, TrustedTime: true})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.st.SettleUsageReservation(ctx, u, "committed", used, payload, 0); err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestCreditCloseWaitsForIndependentPGKnownUsageAndRejectsUnknown(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	ctx := context.Background()
	batch := f.commitLocalUsage(t, 37)
	f.seal(t)
	p, err := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	r := f.localFinal(t, p)
	raw, _ := json.Marshal(r)
	if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, raw, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("final before PG batch", err)
	}
	meter := service.MeteringService{Pool: x.f.pool}
	if _, err = meter.IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 1, batch); err != nil {
		t.Fatal(err)
	}
	if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, raw, uuid.NewString()); err != nil {
		t.Fatal("independent known inventory", err)
	}
	q := generated.New(x.f.pool)
	projection, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || projection.Charged != 44 || projection.F != 18 || projection.RemainderMilli != 400 {
		t.Fatal("parent fractional accounting changed", projection, err)
	}
}

func TestCreditClosePreservesParentUnknownLiability(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	ctx := context.Background()
	f.seal(t)
	p, err := x.s.PrepareCreditClose(ctx, x.f.scope, f.grant, shareBackendID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	r := f.localFinal(t, p)
	raw, _ := json.Marshal(r)
	proof := sha256.Sum256([]byte("unresolved original parent"))
	if err = (service.MeteringService{Pool: x.f.pool}).RecordUnknown(ctx, x.f.scope, f.a.SourceID, proof[:], x.now, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = x.s.RecordCreditClose(x.f.contexts[f.in.NodeID], x.f.scope, raw, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("unknown released", err)
	}
	q := generated.New(x.f.pool)
	if _, err = q.GetCreditCloseReceipt(ctx, generated.GetCreditCloseReceiptParams{TenantID: x.f.tenant, GrantID: f.grant}); err == nil {
		t.Fatal("unknown final committed")
	}
}
