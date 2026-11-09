package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/hub"
)

func TestHubRepeatedSameStoreRecoveryPreservesOriginalParentAndAllEpochIssues(t *testing.T) {
	x, _, hs, a, r := sameStoreFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	ns, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer ns.Close()
	provision, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	initial, e := store.DecodeSourceProvision(provision.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, provision.Payload, store.IssuerTime{Earliest: initial.IssuedAt, Latest: initial.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	installCredit := func(raw []byte) {
		facts, e := agent.InspectAuthorizationUpdate(raw, a.NodeID, a.BootID, a.PathGeneration)
		if e != nil {
			t.Fatal(e)
		}
		grant := ""
		for _, record := range facts.Records {
			if record.Kind == "credit_grant" {
				grant = record.AuthorizationID
			}
		}
		credit, e := agent.InspectIssuedCreditGrant(raw, a.NodeID, a.BootID, a.HubID, grant, a.PathGeneration)
		if e != nil {
			t.Fatal(e)
		}
		handoffs := []store.EpochHandoff{}
		for _, h := range facts.Handoffs {
			if h.SourceID == a.SourceID {
				handoffs = append(handoffs, h)
			}
		}
		digest := sha256.Sum256(raw)
		if e = ns.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{ShareReferences: facts.ShareReferences, IssuerID: a.HubID, BackendHeartbeat: facts.BackendHeartbeat, AutonomyDeadline: facts.AutonomyDeadline, NodeID: a.NodeID, BootID: a.BootID, UpdateID: facts.UpdateID, Payload: raw, Digest: digest[:], SignerEpoch: facts.SignerEpoch, PathGeneration: facts.PathGeneration, FencingGenerationFloor: facts.FencingFloor, Handoffs: handoffs}, []store.AuthorizationRecord{credit}, nil); e != nil {
			t.Fatal("validated local credit install", e)
		}
	}
	priorRows, e := hs.LedgerPrefix(ctx, 0, 1, 10)
	if e != nil || len(priorRows) != 1 {
		t.Fatal(e)
	}
	installCredit(priorRows[0].Payload)
	immutable := []string{"hub_funded_allocations", "reservations", "ledger_events", "ledger_projections", "settlements", "share_generations", "share_chains", "node_source_provisions"}
	original := authoritySnapshot(t, x.f.pool, x.f.tenant, immutable)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	var commands [][]byte
	for cycle := 0; cycle < 3; cycle++ {
		if cycle > 0 {
			x.now = x.now.Add(5 * time.Second)
			r = service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: r.HubID, BackendID: shareBackendID, FromBootID: r.ToBootID, ToBootID: uuid.NewString(), FromEpoch: r.ToEpoch, ToEpoch: r.ToEpoch + 1, Reason: "explicit repeated recovery keeps original source and all historical debt"}
			proof := sha256.Sum256([]byte("explicit next repeated epoch approval"))
			if _, e = x.s.AuthorizeSigner(ctx, admin, r.HubID, r.ToEpoch, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
				t.Fatal(e)
			}
		}
		if cycle == 1 {
			request := uuid.NewString()
			failAudit(t, x.f.pool, request)
			tables := []string{"hub_same_store_recoveries", "operations", "tasks", "outbox", "audit_events"}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			if _, e = x.s.PrepareHubSameStoreRecovery(ctx, admin, r, request); e == nil {
				t.Fatal("repeated approval escaped audit failure")
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("partial repeated approval")
			}
		}
		approved, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString())
		if e != nil {
			t.Fatal("prepare cycle", cycle, e)
		}
		commands = append(commands, approved.Payload)
		var concurrent sync.WaitGroup
		for range 4 {
			concurrent.Go(func() {
				if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); e != nil {
					t.Error(e)
				}
			})
		}
		concurrent.Wait()
		command, e := hub.DecodeSameStoreRecovery(approved.Payload)
		if e != nil {
			t.Fatal(e)
		}
		if cycle > 0 {
			originalCommand, e := hub.DecodeSameStoreRecovery(commands[0])
			if e != nil {
				t.Fatal(e)
			}
			oldLinks, _ := json.Marshal(originalCommand.Allocations)
			newLinks, _ := json.Marshal(command.Allocations)
			if !bytes.Equal(oldLinks, newLinks) {
				t.Fatal("repeated approval rehashed effective roots as original allocation")
			}
		}
		for i, root := range command.Handoff.FundedSuccessors[0].ShareReferences {
			expected := a.ShareReferences[i]
			expected.Revision += uint64(cycle + 1)
			expected.SignerEpoch = uint64(r.ToEpoch)
			if root != expected {
				t.Fatal("effective root changed original limits/deadline", cycle, root, expected)
			}
		}
		_, raw, rows := sameStoreManifest(t, x, hs, r)
		retainSameStore(t, x, r, rows)
		if _, e = x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
			t.Fatal("prefix cycle", cycle, e)
		}
		fence, e := x.s.PrepareNodeRecoveryCreditFence(ctx, admin, r.ID, a.AllocationID, uuid.NewString())
		if e != nil {
			t.Fatal("fence cycle", cycle, e)
		}
		f, e := store.DecodeRecoveryCreditFence(fence.Payload)
		if e != nil || f.Issued != uint64(37+cycle*10) || f.Remaining != uint64(66-cycle*10) || f.Root.Revision != uint64(cycle+1) {
			t.Fatal("original aggregate fence", cycle, f, e)
		}
		recordRecoveryFence(t, x, fence, ns)
		next, e := x.s.PrepareNodeRecoverySources(ctx, admin, r.ID, a.AllocationID, uuid.NewString())
		if e != nil {
			t.Fatal("sources cycle", cycle, e)
		}
		receipt, e := ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, next.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
		if e != nil {
			t.Fatal("Node FULL source cycle", cycle, e)
		}
		if e = x.s.RecordNodeRecoverySources(x.f.contexts[a.NodeID], x.f.scope, receipt, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				if _, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString()); e != nil {
					t.Error("admit cycle", cycle, e)
				}
			})
		}
		wg.Wait()
		delivery, _, e := x.s.HubRecoveryAdmission(x.f.contexts[r.HubID], r.HubID, r.ToBootID, shareBackendID, uuid.NewString())
		if e != nil {
			t.Fatal("delivery cycle", cycle, e)
		}
		if _, e = hs.ApplyHubRecoveryAdmission(ctx, shareBackendID, r.HubID, r.ToBootID, delivery, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil {
			t.Fatal("FULL admission cycle", cycle, e)
		}
		issued, _, sequence := issueAllocationFixture(t, x, hs, a, 10)
		if e = x.s.RecordHubIssue(x.f.contexts[r.HubID], r.HubID, r.ToBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
			t.Fatal("issue cycle", cycle, e)
		}
		installCredit(issued.Payload)
		renewal := x.renewal()
		renewal.SignerEpoch = r.ToEpoch
		if _, e = x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, renewal, uuid.NewString()); e != nil {
			t.Fatal("generation renewal cycle", cycle, e)
		}
		if !bytes.Equal(original, authoritySnapshot(t, x.f.pool, x.f.tenant, immutable)) {
			t.Fatal("recovery changed original funds/capacity", cycle)
		}
	}
	issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issued != 67 {
		t.Fatal("historical epoch issue inventory", issued, e)
	}
	// Every old approval still replays byte-for-byte even after later admissions.
	for _, raw := range commands {
		c, e := hub.DecodeSameStoreRecovery(raw)
		if e != nil {
			t.Fatal(e)
		}
		v, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, service.HubSameStoreRecoveryInput{ID: c.ID, HubID: c.HubID, BackendID: c.BackendID, FromBootID: c.FromBootID, ToBootID: c.ToBootID, FromEpoch: int64(c.Handoff.FromEpoch), ToEpoch: int64(c.Handoff.ToEpoch), Reason: func() string {
			if int64(c.Handoff.FromEpoch) == 7 {
				return "explicit same-store FULL handoff; complete replay still required"
			}
			return "explicit repeated recovery keeps original source and all historical debt"
		}()}, uuid.NewString())
		if e != nil || !bytes.Equal(raw, v.Payload) {
			t.Fatal("historical approval retimed", e)
		}
	}
	// New generation closure also follows the original generation through all3 admissions.
	x.in.SignerEpoch = r.ToEpoch
	next := prepareClosureSuccessor(t, x)
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	pages, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: r.HubID})
	if e != nil || len(pages) != 1 {
		t.Fatal(e, pages)
	}
	closure, e := hs.ApplyShareRenewalClosure(ctx, r.HubID, r.ToBootID, shareBackendID, pages[0].Payload)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(closure)
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[r.HubID], r.HubID, r.ToBootID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal("three epoch closure", e)
	}
}

func TestHubRepeatedPrefixRequiresIssuesFromEveryOriginalEpoch(t *testing.T) {
	x, hs, a, first := effectiveAllocationFixture(t, true)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	issued, _, sequence := issueAllocationFixture(t, x, hs, a, 10)
	if e := x.s.RecordHubIssue(x.f.contexts[a.HubID], a.HubID, first.ToBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.now = x.now.Add(5 * time.Second)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.AuthorizeSigner(ctx, admin, a.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	next := service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: a.HubID, BackendID: shareBackendID, FromBootID: first.ToBootID, ToBootID: uuid.NewString(), FromEpoch: 8, ToEpoch: 9, Reason: "explicit repeated prefix completeness test"}
	m, raw, rows := sameStoreManifest(t, x, hs, next)
	retainSameStore(t, x, next, rows)
	in := generated.MissingHubSameStoreIssuesParams{HubID: a.HubID, HubBootID: next.FromBootID, RecoveryID: next.ID, SealedSequence: m.Result.SealedSequence}
	missing, e := q.MissingHubSameStoreIssues(ctx, in)
	if e != nil || missing {
		t.Fatal("complete historical inventory missing", missing, e)
	}
	// Corrupt retained forensic evidence only; the old-boot PG credit remains.
	if _, e = x.f.pool.Exec(ctx, `ALTER TABLE hub_same_store_journal DISABLE TRIGGER hub_same_store_journal_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = x.f.pool.Exec(ctx, `DELETE FROM hub_same_store_journal WHERE recovery_id=$1 AND sequence=1`, next.ID); e != nil {
		t.Fatal(e)
	}
	missing, e = q.MissingHubSameStoreIssues(ctx, in)
	if e != nil || !missing {
		t.Fatal("omitted earlier-boot issue disappeared from inventory", missing, e)
	}
	tables := []string{"hub_same_store_manifests", "reservations", "ledger_events", "share_chains", "operations", "tasks", "outbox"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.VerifyHubSameStorePrefix(x.f.contexts[a.HubID], a.HubID, next.ToBootID, next.ID, raw, uuid.NewString()); e == nil {
		t.Fatal("incomplete historical prefix accepted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("failed repeated prefix changed authority")
	}
}

func TestHubRepeatedApprovalCannotSkipPriorAdmission(t *testing.T) {
	x, _, a, prior := effectiveAllocationFixture(t, false)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.AuthorizeSigner(ctx, admin, a.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	next := service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: a.HubID, BackendID: shareBackendID, FromBootID: prior.ToBootID, ToBootID: uuid.NewString(), FromEpoch: 8, ToEpoch: 9, Reason: "unadmitted intermediate epoch must not be adopted"}
	tables := []string{"hub_same_store_recoveries", "operations", "tasks", "outbox", "reservations", "share_chains"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, next, uuid.NewString()); e == nil {
		t.Fatal("unadmitted intermediate epoch adopted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("failed repeated approval changed authority")
	}
}
