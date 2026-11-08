package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/contracts"
)

const shareBackendID = "00000000-0000-4000-8000-000000000099"

func shareTask(t *testing.T, x *shareFixture, id string) string {
	t.Helper()
	var task string
	if e := x.f.pool.QueryRow(context.Background(), "SELECT b.task_id FROM share_delivery_bindings b JOIN tasks t ON t.tenant_id=b.tenant_id AND t.id=b.task_id WHERE b.tenant_id=$1 AND b.handoff_id=$2 AND t.state='pending' ORDER BY t.id LIMIT 1", x.f.tenant, id).Scan(&task); e != nil {
		t.Fatal(e)
	}
	return task
}
func leaseShareTask(t *testing.T, x *shareFixture, id string) (string, service.ShareDelivery) {
	t.Helper()
	task := shareTask(t, x, id)
	d, e := x.s.LeaseShareDelivery(context.Background(), x.f.scope, task, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return task, d
}
func nodeHandoff(t *testing.T, x *shareFixture, st *store.Store, d service.ShareDelivery) contracts.ShareObservation {
	t.Helper()
	r, e := st.ApplyShareHandoff(context.Background(), x.in.NodeID, x.in.BootID, shareBackendID, d.Binding.Phase, d.Binding.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func confirmHandoff(t *testing.T, x *shareFixture, st *store.Store, id string) contracts.ShareObservation {
	t.Helper()
	task, d := leaseShareTask(t, x, id)
	r := nodeHandoff(t, x, st, d)
	if e := x.s.RecordShareReceipt(x.f.contexts[x.in.NodeID], x.f.scope, task, d.Token, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return r
}
func shareChain(t *testing.T, x *shareFixture) generated.ShareChain {
	t.Helper()
	r, e := (repository.ShareRepository{Pool: x.f.pool}).Chain(context.Background(), x.f.scope, x.in.NodeID, x.in.Kind, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func closePredecessor(t *testing.T, x *shareFixture) {
	t.Helper()
	in := service.RenewalClosure{NodeID: x.in.NodeID, BootID: x.in.BootID, Kind: x.in.Kind, SignerID: x.in.SignerID, Generation: x.in.ExpectedGeneration, SuccessorGeneration: x.in.ExpectedGeneration + 1, SignerEpoch: x.in.SignerEpoch}
	if e := x.s.CloseOldRenewal(x.f.contexts[x.in.SignerID], x.f.scope, in, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}
func TestShareHandoffLostReceiptRestartRetainsAndConfirmsExactDifference(t *testing.T) {
	x := newShareFixture(t)
	path := filepath.Join(t.TempDir(), "node.db")
	st, e := store.Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { st.Close() }()
	p := x.prepare(t)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	task, d := leaseShareTask(t, x, p.Generation.ID)
	initial := d
	r := nodeHandoff(t, x, st, d)
	c := shareChain(t, x)
	if c.ActiveGeneration != 0 || c.HeldAmount != 600000 {
		t.Fatal("send became proof", c)
	}
	if e = x.s.ShareDeliveryUnknown(context.Background(), x.f.scope, task, d.Token, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = store.Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	r = confirmHandoff(t, x, st, p.Generation.ID)
	if r.ActiveGeneration != 1 || r.MinimumGeneration != 1 || shareChain(t, x).ActiveGeneration != 1 {
		t.Fatal("durable floor not recovered", r)
	}
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	next := x.prepare(t)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, next.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("CAS before old signer closed", e)
	}
	closePredecessor(t, x)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	task, d = leaseShareTask(t, x, next.Generation.ID)
	r = nodeHandoff(t, x, st, d)
	if shareChain(t, x).HeldAmount != 600000 {
		t.Fatal("CAS before receipt released")
	}
	bad := r
	bad.ActiveGeneration = 1
	if e = x.s.RecordShareReceipt(x.f.contexts[x.in.NodeID], x.f.scope, task, d.Token, bad, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("low actual generation released", e)
	}
	if e = x.s.RecordShareReceipt(context.Background(), x.f.scope, task, d.Token, r, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("forged receipt", e)
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_chains", "share_handoffs", "share_handoff_receipts", "share_delivery_bindings", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordShareReceipt(x.f.contexts[x.in.NodeID], x.f.scope, task, d.Token, r, request); e == nil {
		t.Fatal("receipt audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial difference release")
	}
	for i := 0; i < 2; i++ {
		if e = x.s.RecordShareReceipt(x.f.contexts[x.in.NodeID], x.f.scope, task, d.Token, r, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	c = shareChain(t, x)
	if c.ActiveGeneration != 2 || c.PendingGeneration != 0 || c.HeldAmount != 300000 {
		t.Fatal("difference or receipt duplicated", c)
	}
	// Replaying the first generation after a higher durable floor cannot change
	// the Node head or the PostgreSQL head.
	if _, e = st.ApplyShareHandoff(context.Background(), x.in.NodeID, x.in.BootID, shareBackendID, "cas", initial.Binding.Command, store.IssuerTime{}); e != nil {
		t.Fatal("exact persisted replay should return original result", e)
	}
}
func TestShareHandoffCrashBeforeSendQueriesWithoutFreeingBootstrap(t *testing.T) {
	x := newShareFixture(t)
	st, e := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	p := x.prepare(t)
	id := p.Generation.ID
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_handoffs", "share_delivery_bindings", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, id, request); e == nil {
		t.Fatal("queue audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial CAS task")
	}
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, id, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	task := shareTask(t, x, id)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.LeaseShareDelivery(context.Background(), x.f.scope, task, request); e == nil {
		t.Fatal("delivery audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial send permission")
	}
	task, d := leaseShareTask(t, x, id)
	// No call to the Node: crash after commit, before send.
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.ShareDeliveryUnknown(context.Background(), x.f.scope, task, d.Token, request); e == nil {
		t.Fatal("unknown audit failure accepted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial unknown transition")
	}
	if e = x.s.ShareDeliveryUnknown(context.Background(), x.f.scope, task, d.Token, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := confirmHandoff(t, x, st, id)
	if r.State != "unknown" || shareChain(t, x).HeldAmount != 600000 || shareChain(t, x).ActiveGeneration != 0 {
		t.Fatal("missing became never activated proof", r)
	}
	x.now = x.now.Add(48 * time.Hour)
	// No time-based path releases an unknown share, even after all deadlines.
	h, e := generated.New(x.f.pool).GetShareHandoff(context.Background(), generated.GetShareHandoffParams{TenantID: x.f.tenant, ID: id})
	if e != nil || h.State != "activation-unknown" || shareChain(t, x).HeldAmount != 600000 {
		t.Fatal(e, h)
	}
}

func installRuntimeForShare(t *testing.T, x *shareFixture, st *store.Store) store.AuthorizationUse {
	t.Helper()
	b, e := generated.New(x.f.pool).GetBillingSnapshot(context.Background(), generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	at := x.now.Add(time.Second)
	grant := store.AuthorizationRecord{AuthorizationID: uuid.NewString(), ParentAuthorizationID: uuid.NewString(), SourceID: uuid.NewString(), Kind: "credit_grant", RuleID: b.RuleID, RuleRevision: uint64(b.RuleRevision), PathGeneration: 1, SignerEpoch: 7, BootID: x.in.BootID, IssuedAt: x.now.Format(time.RFC3339Nano), NotBefore: x.now.Format(time.RFC3339Nano), ExpiresAt: x.now.Add(5 * time.Minute).Format(time.RFC3339Nano), HardDeadline: x.now.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: b.ID, AmountBytes: 1000000}
	runtime := grant
	runtime.AuthorizationID = uuid.NewString()
	runtime.Kind = "runtime_lease"
	runtime.AmountBytes = 0
	runtime.BillingSnapshotID = ""
	sh := runtime
	sh.AuthorizationID = uuid.NewString()
	sh.Kind = "share_lease"
	sh.SourceID = uuid.NewString()
	sh.RateBPS = 1000000
	sh.Connections = 10
	if e = st.PersistAuthorizationUpdate(context.Background(), store.AuthorizationUpdate{IssuerID: x.in.SignerID, NodeID: x.in.NodeID, BootID: x.in.BootID, UpdateID: uuid.NewString(), Payload: []byte("funded test authority"), SignerEpoch: 7, PathGeneration: 1, FencingGenerationFloor: 3}, []store.AuthorizationRecord{grant, runtime, sh}, nil); e != nil {
		t.Fatal(e)
	}
	return store.AuthorizationUse{NodeID: x.in.NodeID, BootID: x.in.BootID, RuleID: b.RuleID, RuleRevision: uint64(b.RuleRevision), PathGeneration: 1, FencingGeneration: 3, Earliest: at, Latest: at, TrustedTime: true}
}
func TestShareHandoffConnectionReductionWaitsForRealRuntimeDrain(t *testing.T) {
	x := newShareFixture(t)
	st, e := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	for _, kind := range []string{"rate", "connections"} {
		x.in.Kind = kind
		x.in.Nonce = uuid.NewString()
		if kind == "connections" {
			x.in.Amount = 4
			x.in.BurstBytes = 0
		}
		p := x.prepare(t)
		if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		confirmHandoff(t, x, st, p.Generation.ID)
	}
	use := installRuntimeForShare(t, x, st)
	sessions := []string{}
	for i := 0; i < 4; i++ {
		id := uuid.NewString()
		sessions = append(sessions, id)
		if _, e = st.OpenRuntimeSession(context.Background(), use, id, id); e != nil {
			t.Fatal(e)
		}
	}
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 2
	p := x.prepare(t)
	closePredecessor(t, x)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := confirmHandoff(t, x, st, p.Generation.ID)
	if r.State != "draining" || r.OpenConnections != 4 || shareChain(t, x).HeldAmount != 4 {
		t.Fatal("connection reduction released before drain", r)
	}
	renew := x.renewal()
	renew.Generation = 2
	if _, e = x.s.RenewShare(x.f.contexts[x.in.NodeID], x.f.scope, renew, uuid.NewString()); e != nil {
		t.Fatal("draining successor cannot renew", e)
	}
	if _, e = st.OpenRuntimeSession(context.Background(), use, uuid.NewString(), uuid.NewString()); e == nil {
		t.Fatal("over-limit new session accepted")
	}
	// Close actual persisted sessions, not a synthetic claimed count.
	for _, id := range sessions[:2] {
		if e = st.CloseRuntimeSession(context.Background(), use.NodeID, use.BootID, id); e != nil {
			t.Fatal(e)
		}
	}
	r = confirmHandoff(t, x, st, p.Generation.ID)
	if r.State != "enforced" || r.OpenConnections != 2 || shareChain(t, x).HeldAmount != 2 || shareChain(t, x).ActiveGeneration != 2 {
		t.Fatal("drained result did not release exact difference", r)
	}
}

func TestShareHandoffRateCapAcrossSessionsDirectionsAndNoCASRefill(t *testing.T) {
	x := newShareFixture(t)
	st, e := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	for _, kind := range []string{"rate", "connections"} {
		x.in.Kind = kind
		x.in.Nonce = uuid.NewString()
		if kind == "connections" {
			x.in.Amount = 3
			x.in.BurstBytes = 0
		}
		p := x.prepare(t)
		if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		confirmHandoff(t, x, st, p.Generation.ID)
	}
	use := installRuntimeForShare(t, x, st)
	sessions := []store.RuntimeAuthorization{}
	for _, flow := range []string{"one", "two"} {
		a, e := st.OpenRuntimeSession(context.Background(), use, flow, flow)
		if e != nil {
			t.Fatal(e)
		}
		sessions = append(sessions, a)
	}
	reserve := func(a store.RuntimeAuthorization, flow, direction string, n uint64) error {
		u := a.Use
		u.Earliest = use.Earliest
		u.Latest = use.Latest
		_, e := st.ReserveRuntimeUsage(context.Background(), store.UsageReservation{NodeID: u.NodeID, BootID: u.BootID, BatchID: uuid.NewString(), AuthorizationID: u.AuthorizationID, SnapshotID: u.BillingSnapshotID, FlowKey: flow, RuleID: u.RuleID, Direction: direction, Reserved: n}, u)
		return e
	}
	if e = reserve(sessions[0], "one", "ingress", 40000); e != nil {
		t.Fatal(e)
	}
	var limited *store.RateLimited
	if e = reserve(sessions[1], "two", "egress", 40000); !errors.As(e, &limited) {
		t.Fatal("both directions exceeded node aggregate 75000-byte burst", e)
	}
	if e = reserve(sessions[1], "two", "egress", 35000); e != nil {
		t.Fatal("failed reservation consumed tokens", e)
	}
	x.in.Kind = "rate"
	x.in.ExpectedGeneration = 1
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	x.in.Nonce = uuid.NewString()
	p := x.prepare(t)
	closePredecessor(t, x)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	confirmHandoff(t, x, st, p.Generation.ID)
	if e = reserve(sessions[0], "one", "ingress", 1); !errors.As(e, &limited) {
		t.Fatal("CAS refilled exhausted tenant bucket", e)
	}
	use.Earliest = use.Earliest.Add(time.Second)
	use.Latest = use.Earliest
	if e = reserve(sessions[0], "one", "ingress", 37500); e != nil {
		t.Fatal("new lower rate did not earn bounded tokens", e)
	}
}

func TestShareHandoffNodeReceiptWriteFailureRollsBackCapacityAndFloor(t *testing.T) {
	x := newShareFixture(t)
	path := filepath.Join(t.TempDir(), "node.db")
	st, e := store.Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	p := x.prepare(t)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	confirmHandoff(t, x, st, p.Generation.ID)
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	p = x.prepare(t)
	closePredecessor(t, x)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	task, d := leaseShareTask(t, x, p.Generation.ID)
	disk, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer disk.Close()
	if _, e = disk.Exec("CREATE TRIGGER fail_share_receipt BEFORE INSERT ON node_share_commands BEGIN SELECT RAISE(ABORT,'injected receipt storage failure'); END"); e != nil {
		t.Fatal(e)
	}
	result, e := st.ApplyShareHandoff(context.Background(), x.in.NodeID, x.in.BootID, shareBackendID, "cas", d.Binding.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true})
	if e == nil || result.State == "enforced" {
		t.Fatal("write failure acknowledged capacity", e, result)
	}
	var generation int64
	if e = disk.QueryRow("SELECT generation FROM node_share_heads WHERE tenant_id=? AND node_id=? AND kind='rate'", x.f.tenant, x.in.NodeID).Scan(&generation); e != nil || generation != 1 {
		t.Fatal("floor escaped failed receipt transaction", e, generation)
	}
	if shareChain(t, x).HeldAmount != 600000 {
		t.Fatal("failed Node CAS released difference")
	}
	if _, e = disk.Exec("DROP TRIGGER fail_share_receipt"); e != nil {
		t.Fatal(e)
	}
	r := nodeHandoff(t, x, st, d)
	if e = x.s.RecordShareReceipt(x.f.contexts[x.in.NodeID], x.f.scope, task, d.Token, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if shareChain(t, x).HeldAmount != 300000 {
		t.Fatal("successful retry did not release exact difference")
	}
}

func TestShareHandoffRejectsChangedCommandOldBootAndLowerFence(t *testing.T) {
	x := newShareFixture(t)
	st, e := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	p := x.prepare(t)
	if e = x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	_, d := leaseShareTask(t, x, p.Generation.ID)
	nodeHandoff(t, x, st, d)
	c, e := contracts.DecodeShareHandoff(d.Binding.Command)
	if e != nil {
		t.Fatal(e)
	}
	changed := c
	changed.Amount--
	raw, _ := json.Marshal(changed)
	if r, e := st.ApplyShareHandoff(context.Background(), c.NodeID, c.BootID, shareBackendID, "cas", raw, store.IssuerTime{}); e == nil || r.State != "" {
		t.Fatal("mutated exact replay", r, e)
	}
	changed = c
	changed.ID = uuid.NewString()
	changed.Predecessor = 1
	changed.Generation = 2
	changed.ClosureDigest = contracts.SharePayloadDigest([]byte("closure"))
	changed.BootID = uuid.NewString()
	raw, _ = json.Marshal(changed)
	now := store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}
	if r, e := st.ApplyShareHandoff(context.Background(), c.NodeID, changed.BootID, shareBackendID, "cas", raw, now); e == nil || r.State != "" {
		t.Fatal("new boot reused old chain", r, e)
	}
	changed.BootID = c.BootID
	changed.FencingGeneration = c.FencingGeneration - 1
	raw, _ = json.Marshal(changed)
	if r, e := st.ApplyShareHandoff(context.Background(), c.NodeID, c.BootID, shareBackendID, "cas", raw, now); e == nil || r.State != "" {
		t.Fatal("lower fence accepted", r, e)
	}
}
