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
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func sameStoreManifest(t *testing.T, x *shareFixture, hs *store.Store, r service.HubSameStoreRecoveryInput) (hub.SameStoreRecoveryManifest, []byte, []store.LedgerRecord) {
	t.Helper()
	ctx := context.Background()
	a, e := x.s.PrepareHubSameStoreRecovery(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	c, e := hub.DecodeSameStoreRecovery(a.Payload)
	if e != nil {
		t.Fatal(e)
	}
	result, e := hs.HandoffHub(ctx, c.Handoff, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	m := hub.SameStoreRecoveryManifest{Request: c.Handoff, Result: result}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	records, e := hs.LedgerPrefix(ctx, 0, result.SealedSequence+1, 1000)
	if e != nil {
		t.Fatal(e)
	}
	return m, raw, records
}

func retainSameStore(t *testing.T, x *shareFixture, r service.HubSameStoreRecoveryInput, records []store.LedgerRecord) {
	t.Helper()
	for _, rec := range records {
		if e := x.s.RetainHubSameStoreRecord(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, rec, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
}

func TestHubSameStorePrefixCompleteFinancialInventoryRollbackAndReplay(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	old, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(old) != 1 {
		t.Fatal(old, e)
	}
	if _, e = hs.ConfirmLedger(ctx, old[0], old[0].Digest); e != nil {
		t.Fatal(e)
	}
	m, raw, records := sameStoreManifest(t, x, hs, r)
	if len(records) != 2 || records[0].Kind != "issue" {
		t.Fatal("confirmed original row not retained", records)
	}
	tables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "share_renewal_authorities", "settlements"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	all := []string{"hub_same_store_journal", "hub_same_store_manifests", "operations", "tasks", "outbox", "audit_events"}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, all)
	if e = x.s.RetainHubSameStoreRecord(ctx, r.HubID, r.ToBootID, r.ID, records[0], request); e == nil {
		t.Fatal("retention escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, all)) {
		t.Fatal("partial retained obligation")
	}
	// Reverse delivery is allowed, but a handoff hash alone is not a prefix.
	retainSameStore(t, x, r, records[1:])
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("hole accepted", e)
	}
	retainSameStore(t, x, r, records[:1])
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, all)
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, request); e == nil {
		t.Fatal("manifest escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, all)) {
		t.Fatal("partial verified manifest")
	}
	result, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString())
	if e != nil || result.SealedSequence != m.Result.SealedSequence {
		t.Fatal(result, e)
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("verification changed original money or capacity")
	}
	current, e := generated.New(x.f.pool).LatestHubIncarnation(ctx, r.HubID)
	if e != nil || current.SignerEpoch != in.SignerEpoch || current.BootID != in.HubBootID {
		t.Fatal("verified prefix admitted signer", current, e)
	}
	if _, e = x.s.BindHubSession(ctx, r.HubID, r.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("verification bypassed admission", e)
	}
	// Durable evidence is not an ACK: local credit still cannot be issued.
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e == nil {
		t.Fatal("unacknowledged prefix minted")
	}
	x.s.Clock = nil
	replay, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString())
	if e != nil || !bytes.Equal(replay.Payload, result.Payload) || replay.OperationID != result.OperationID {
		t.Fatal("manifest replay changed", e)
	}
	retainSameStore(t, x, r, records)
	extra := records[0]
	extra.Sequence = 3
	extra.ID = uuid.NewString()
	if e = x.s.RetainHubSameStoreRecord(ctx, r.HubID, r.ToBootID, r.ID, extra, uuid.NewString()); e == nil {
		t.Fatal("new post-manifest row")
	}
	for _, table := range []string{"hub_same_store_journal", "hub_same_store_manifests"} {
		if _, e = x.f.pool.Exec(ctx, "DELETE FROM "+table+" WHERE recovery_id=$1", r.ID); e == nil {
			t.Fatal("mutable witness", table)
		}
	}
}

func TestHubSameStorePrefixRequiresPGIssueRegistration(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	issue, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 2, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	x.now = x.now.Add(time.Second)
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("unregistered child accepted", e)
	}
	var seq int64
	for _, v := range records {
		if bytes.Equal(v.Payload, issue.Payload) {
			seq = v.Sequence
		}
	}
	if e = x.s.RecordHubIssue(ctx, r.HubID, in.HubBootID, seq, issue.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}

func TestHubSameStorePrefixForgeryScopeAndConcurrentReplay(t *testing.T) {
	x, _, hs, _, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	m, raw, records := sameStoreManifest(t, x, hs, r)
	if e := x.s.RetainHubSameStoreRecord(context.Background(), r.HubID, r.ToBootID, r.ID, records[0], uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned retention", e)
	}
	if e := x.s.RetainHubSameStoreRecord(ctx, r.HubID, r.FromBootID, r.ID, records[0], uuid.NewString()); e == nil {
		t.Fatal("wrong boot retained")
	}
	bad := records[0]
	bad.Digest = make([]byte, 32)
	if e := x.s.RetainHubSameStoreRecord(ctx, r.HubID, r.ToBootID, r.ID, bad, uuid.NewString()); e == nil {
		t.Fatal("wrong digest retained")
	}
	retainSameStore(t, x, r, records)
	bad = records[0]
	bad.ID = uuid.NewString()
	if e := x.s.RetainHubSameStoreRecord(ctx, r.HubID, r.ToBootID, r.ID, bad, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("sequence rewritten", e)
	}
	m.Result.FundedInventory.Delegations[0].Issued++
	m.Result.FundedInventory.Delegations[0].Remaining--
	m.Result.Remaining[m.Result.FundedInventory.Delegations[0].DelegationID]--
	forged, _ := json.Marshal(m)
	if _, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, forged, uuid.NewString()); e == nil {
		t.Fatal("forged balanced partition accepted")
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString())
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e := x.f.pool.QueryRow(ctx, `SELECT count(*) FROM hub_same_store_manifests WHERE recovery_id=$1`, r.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}

func TestHubSameStorePrefixRechecksAuthorityAndExpiry(t *testing.T) {
	for _, name := range []string{"expiry", "advance", "new-tenant"} {
		t.Run(name, func(t *testing.T) {
			x, _, hs, _, r := sameStoreFixture(t)
			ctx := x.f.contexts[r.HubID]
			_, raw, records := sameStoreManifest(t, x, hs, r)
			retainSameStore(t, x, r, records)
			proof := sha256.Sum256([]byte("authority changed after command"))
			switch name {
			case "expiry":
				x.now = x.now.Add(5 * time.Minute)
			case "advance":
				if _, e := x.s.AuthorizeSigner(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "new-tenant":
				other := uuid.NewString()
				if _, e := x.f.pool.Exec(ctx, `INSERT INTO tenants(id,account_state,version) VALUES($1,'active',1)`, other); e != nil {
					t.Fatal(e)
				}
				if _, e := x.s.AuthorizeSigner(ctx, policyScope(t, other, auth.PlatformAdmin, policy.Grant), r.HubID, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e == nil {
				t.Fatal("stale authority accepted")
			}
		})
	}
}

func TestHubSameStorePrefixIndependentlyRejectsBalancedManifestForgery(t *testing.T) {
	x, _, hs, _, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	m, _, records := sameStoreManifest(t, x, hs, r)
	// The transmitted handoff record itself is self-consistent and matches its
	// SHA. Only reconstruction from independent PG child receipts can reject it.
	p := &m.Result.FundedInventory.Delegations[0]
	p.Issued++
	p.Remaining--
	m.Result.Remaining[p.DelegationID]--
	raw, _ := json.Marshal(m)
	h := sha256.Sum256(raw)
	last := len(records) - 1
	records[last].Payload = raw
	records[last].Digest = h[:]
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("balanced local forgery trusted instead of PG receipts", e)
	}
}

func TestHubSameStorePrefixPagesOverIndependentPGInventory(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	for i := 0; i < 40; i++ {
		issued, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
		if e != nil {
			t.Fatal(e)
		}
		seq := int64(i + 2)
		if e = x.s.RecordHubIssue(ctx, r.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	x.now = x.now.Add(time.Second)
	m, raw, records := sameStoreManifest(t, x, hs, r)
	if len(records) != 42 || m.Result.FundedInventory.Delegations[0].Issued != 77 {
		t.Fatal("bad fixture partition", len(records), m)
	}
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal("cross-page verification", e)
	}
}

func TestHubSameStorePrefixCannotOmitIndependentPGChild(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	issued, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 2, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordHubIssue(ctx, r.HubID, in.HubBootID, 2, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.now = x.now.Add(time.Second)
	m, _, records := sameStoreManifest(t, x, hs, r)
	// Forge a contiguous shorter prefix and perfectly balanced first-child-only
	// partition. The independently registered second PG child must still deny it.
	first := records[0]
	h := sha256.New()
	enc := json.NewEncoder(h)
	if e = enc.Encode(struct {
		Sequence         int64
		Kind, ID, SHA256 string
	}{first.Sequence, first.Kind, first.ID, hex.EncodeToString(first.Digest)}); e != nil {
		t.Fatal(e)
	}
	digest, _, e := contracts.PartitionDigest([]contracts.IssuedPartition{{ID: first.ID, Digest: hex.EncodeToString(first.Digest), Raw: 37}})
	if e != nil {
		t.Fatal(e)
	}
	m.Result.SealedSequence = 1
	m.Result.FundedInventory.PrefixCount = 1
	m.Result.FundedInventory.PrefixSHA256 = hex.EncodeToString(h.Sum(nil))
	p := &m.Result.FundedInventory.Delegations[0]
	p.Issued = 37
	p.Remaining = 66
	p.IssueCount = 1
	p.IssueSHA256 = digest
	m.Result.Remaining[p.DelegationID] = 66
	raw, _ := json.Marshal(m)
	sum := sha256.Sum256(raw)
	last := records[len(records)-1]
	last.Sequence = 2
	last.Payload = raw
	last.Digest = sum[:]
	retainSameStore(t, x, r, []store.LedgerRecord{first, last})
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("independent PG child silently omitted", e)
	}
}

func TestHubSameStorePrefixRetentionDoesNotSettleUsage(t *testing.T) {
	x, _, hs, a, r := sameStoreFixture(t)
	ctx := x.f.contexts[r.HubID]
	id := uuid.NewString()
	raw, e := proto.Marshal(&controlv1.UsageBatch{BatchId: id, BillingSnapshotId: a.SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: a.RuleID, GrantId: uuid.NewString(), Role: "source", Direction: "ingress", Bytes: 7}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = hs.PersistUsageLedger(ctx, store.UsageBatch{NodeID: a.NodeID, BootID: a.BootID, BatchID: id, SnapshotID: a.SnapshotID, Payload: raw}); e != nil {
		t.Fatal(e)
	}
	if e = hs.RecordUsageOrigin(ctx, a.NodeID, id, a.BootID, 1, raw); e != nil {
		t.Fatal(e)
	}
	_, manifest, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	before, e := hs.PendingBackendUsage(ctx)
	if e != nil || len(before) != 1 {
		t.Fatal(len(before), e)
	}
	if _, e = x.s.VerifyHubSameStorePrefix(ctx, r.HubID, r.ToBootID, r.ID, manifest, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	after, e := hs.PendingBackendUsage(ctx)
	if e != nil || len(after) != 1 {
		t.Fatal("prefix witness fabricated a usage ACK", len(after), e)
	}
	var count int
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM usage_batches WHERE tenant_id=$1`, x.f.tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("retention masqueraded as settlement", count, e)
	}
}
