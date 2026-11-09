package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFundedHandoffRequiresExplicitRootSuccessors(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	h := HandoffRequest{ID: shareID(850), HubID: d.HubID, FromEpoch: d.SignerEpoch, ToEpoch: d.SignerEpoch + 1, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest, clock.Latest = h.Heartbeat, h.Heartbeat.Add(time.Second)
	if _, err := s.HandoffHub(context.Background(), h, clock); err == nil {
		t.Fatal("funded ownership moved without explicit backend root successors")
	}
	b, err := s.AuditHubDelegation(context.Background(), d.ID)
	if err != nil || b.Issued != 0 || b.Remaining != d.RawBytes {
		t.Fatalf("rejected handoff changed funding: %+v %v", b, err)
	}
}

func fundedHandoffRequest(t *testing.T, d HubDelegation) (HandoffRequest, IssuerTime) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	next := append([]ShareReference(nil), d.ShareReferences...)
	for i := range next {
		next[i].Revision++
		next[i].SignerEpoch++
	}
	h := HandoffRequest{ID: shareID(850), HubID: d.HubID, FromEpoch: d.SignerEpoch, ToEpoch: d.SignerEpoch + 1, Heartbeat: d.BackendHeartbeat.Add(time.Minute), BackendID: shareID(799), FundedSuccessors: []FundedHandoffSuccessor{{DelegationID: d.ID, AllocationSHA256: hex.EncodeToString(hash[:]), ShareReferences: next}}}
	return h, IssuerTime{Earliest: h.Heartbeat, Latest: h.Heartbeat.Add(time.Second), Trusted: true}
}

func completeFundedHandoff(t *testing.T, s *Store, clock IssuerTime) {
	t.Helper()
	ctx := context.Background()
	records, err := s.PendingLedger(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if _, err = s.ConfirmLedger(ctx, r, r.Digest); err != nil {
			t.Fatal(err)
		}
	}
	if done, err := s.CompleteRecovery(ctx, clock); err != nil || !done {
		t.Fatalf("barrier: %v %v", done, err)
	}
}

func TestFundedHandoffReopenBarrierRootsLeasesAndCompleteSeal(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	old, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 851), clock)
	if err != nil {
		t.Fatal(err)
	}
	h, clock := fundedHandoffRequest(t, d)
	result, err := s.HandoffHub(ctx, h, clock)
	if err != nil || result.Remaining[d.ID] != 768 || result.SealedSequence != 1 {
		t.Fatalf("handoff %+v %v", result, err)
	}
	if result.FundedInventory == nil || result.FundedInventory.PrefixCount != 1 || len(result.FundedInventory.PrefixSHA256) != 64 || len(result.FundedInventory.Delegations) != 1 {
		t.Fatalf("missing complete manifest: %+v", result.FundedInventory)
	}
	partition := result.FundedInventory.Delegations[0]
	if partition.Raw != 1024 || partition.Issued != 256 || partition.Remaining != 768 || partition.IssueCount != 1 {
		t.Fatalf("incomplete original partition: %+v", partition)
	}
	if _, err = s.IssueHubAuthorization(ctx, issuerRequest(d, 852), clock); err == nil {
		t.Fatal("unconfirmed prefix minted credit")
	}
	if _, err = s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); err == nil {
		t.Fatal("unconfirmed prefix minted lease")
	}
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if replay, err := other.HandoffHub(ctx, h, IssuerTime{}); err != nil || replay.Remaining[d.ID] != 768 {
		t.Fatalf("FULL handoff replay: %+v %v", replay, err)
	}
	rows, err := s.PendingLedger(ctx, 100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("journal %d %v", len(rows), err)
	}
	if _, err = s.ConfirmLedger(ctx, rows[1], rows[1].Digest); err != nil {
		t.Fatal(err)
	}
	if done, err := s.CompleteRecovery(ctx, clock); err != nil || done {
		t.Fatalf("hole skipped: %v %v", done, err)
	}
	completeFundedHandoff(t, other, clock)
	if replay, err := other.IssueHubAuthorization(ctx, issuerRequest(d, 851), clock); err != nil || !bytes.Equal(old.Payload, replay.Payload) {
		t.Fatalf("old issue changed: %v", err)
	}
	next, err := other.IssueHubAuthorization(ctx, issuerRequest(d, 852), clock)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Schema   string           `json:"schema_version"`
		Epoch    uint64           `json:"signer_epoch"`
		Refs     []ShareReference `json:"share_references"`
		Handoffs []EpochHandoff   `json:"epoch_handoffs"`
	}
	if err = json.Unmarshal(next.Payload, &p); err != nil || p.Schema != "1.2.0" || p.Epoch != 2 || len(p.Refs) != 3 || len(p.Handoffs) != 3 {
		t.Fatalf("lost funded restrictions: %s %v", next.Payload, err)
	}
	for i, r := range p.Refs {
		if r.Revision != 2 || r.Generation != d.ShareReferences[i].Generation || r.SourceID != d.ShareReferences[i].SourceID || !r.HardDeadline.Equal(d.ShareReferences[i].HardDeadline) {
			t.Fatalf("root widened: %+v", r)
		}
	}
	if p.Handoffs[0].Remaining != 768 || p.Handoffs[1].Remaining != 0 || p.Handoffs[2].Remaining != 0 {
		t.Fatalf("bad R %+v", p.Handoffs)
	}
	clock.Earliest, clock.Latest = h.Heartbeat.Add(62*time.Second), h.Heartbeat.Add(63*time.Second)
	lease, err := other.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock)
	if err != nil || json.Unmarshal(lease.Payload, &p) != nil || p.Epoch != 2 || len(p.Handoffs) != 3 {
		t.Fatalf("lease: %s %v", lease.Payload, err)
	}
	seal, err := other.SealHubDelegation(ctx, d.HubID, shareID(853), d.ID)
	if err != nil || seal.SignerEpoch != 2 || seal.Raw != 1024 || seal.Issued != 512 || seal.Unissued != 512 || seal.IssueCount != 2 {
		t.Fatalf("seal lost history %+v %v", seal, err)
	}
	var original []byte
	if err = s.db.QueryRow(`SELECT payload FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(d)
	if !bytes.Equal(original, want) {
		t.Fatal("original allocation rewritten")
	}
}

func TestFundedHandoffCorruptOrMissingHistoryCannotTransfer(t *testing.T) {
	for _, name := range []string{"prefix-hole", "issue-payload", "issue-amount", "remaining", "prefix-payload", "balanced-column-tamper"} {
		t.Run(name, func(t *testing.T) {
			s, d, clock, _ := renewalIssuerFixture(t)
			ctx := context.Background()
			if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 880), clock); err != nil {
				t.Fatal(err)
			}
			stmt := ""
			switch name {
			case "prefix-hole":
				stmt = `DELETE FROM hub_ledger WHERE kind='issue'`
			case "issue-payload":
				stmt = `UPDATE issuer_issues SET payload=x'00'`
			case "issue-amount":
				stmt = `UPDATE issuer_issues SET amount_bytes='255'`
			case "remaining":
				stmt = `UPDATE issuer_delegations SET remaining_bytes='769'`
			case "prefix-payload":
				stmt = `UPDATE hub_ledger SET payload=x'00'`
			case "balanced-column-tamper":
				stmt = `UPDATE issuer_issues SET amount_bytes='255'; UPDATE issuer_delegations SET remaining_bytes='769'`
			}
			if _, err := s.db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
			h, clock := fundedHandoffRequest(t, d)
			if _, err := s.HandoffHub(ctx, h, clock); err == nil {
				t.Fatal("unproven inventory transferred")
			}
			var epoch string
			if err := s.db.QueryRow(`SELECT signer_epoch FROM issuer_epochs`).Scan(&epoch); err != nil || epoch != "1" {
				t.Fatalf("partial transfer %s %v", epoch, err)
			}
		})
	}
}

func TestFundedHandoffAttestationsAreImmutable(t *testing.T) {
	s, d, _, _ := renewalIssuerFixture(t)
	h, clock := fundedHandoffRequest(t, d)
	if _, err := s.HandoffHub(context.Background(), h, clock); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`UPDATE hub_handoffs SET request=x'00'`, `DELETE FROM hub_handoffs`, `UPDATE hub_transfers SET remaining_bytes='1024'`, `DELETE FROM hub_transfers`, `UPDATE hub_ledger SET payload=x'00' WHERE kind='handoff'`, `DELETE FROM hub_ledger WHERE kind='handoff'`} {
		if _, err := s.db.Exec(stmt); err == nil {
			t.Fatal("mutable evidence", stmt)
		}
	}
	completeFundedHandoff(t, s, clock)
}

func TestFundedHandoffRejectsScopeMutationAndRollsBackRoots(t *testing.T) {
	for _, name := range []string{"backend", "hash", "source", "epoch", "revision", "generation", "rate", "connections", "deadline", "extra", "duplicate", "missing", "journal-failure"} {
		t.Run(name, func(t *testing.T) {
			s, d, _, _ := renewalIssuerFixture(t)
			h, clock := fundedHandoffRequest(t, d)
			root := &h.FundedSuccessors[0].ShareReferences[0]
			switch name {
			case "backend":
				h.BackendID = shareID(798)
			case "hash":
				h.FundedSuccessors[0].AllocationSHA256 = hex.EncodeToString(make([]byte, 32))
			case "source":
				root.SourceID = shareID(860)
			case "epoch":
				root.SignerEpoch++
			case "revision":
				root.Revision++
			case "generation":
				root.Generation++
			case "rate":
				root.RateBPS++
			case "connections":
				root.Connections++
			case "deadline":
				root.HardDeadline = root.HardDeadline.Add(time.Second)
			case "extra":
				h.FundedSuccessors = append(h.FundedSuccessors, FundedHandoffSuccessor{DelegationID: shareID(862)})
			case "duplicate":
				h.FundedSuccessors = append(h.FundedSuccessors, h.FundedSuccessors[0])
			case "missing":
				h.FundedSuccessors[0].ShareReferences = h.FundedSuccessors[0].ShareReferences[:2]
			case "journal-failure":
				if _, err := s.db.Exec(`CREATE TRIGGER fail_funded_handoff BEFORE INSERT ON hub_ledger WHEN NEW.kind='handoff' BEGIN SELECT RAISE(ABORT,'FULL failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.HandoffHub(context.Background(), h, clock); err == nil {
				t.Fatal("invalid transfer accepted")
			}
			var epoch string
			var roots int
			if err := s.db.QueryRow(`SELECT signer_epoch FROM issuer_epochs`).Scan(&epoch); err != nil || epoch != "1" {
				t.Fatalf("epoch leaked %s %v", epoch, err)
			}
			if err := s.db.QueryRow(`SELECT count(*) FROM share_source_anchors`).Scan(&roots); err != nil || roots != 3 {
				t.Fatalf("roots leaked %d %v", roots, err)
			}
		})
	}
}

func TestFundedHandoffConcurrentSingleOwnerAndSecondEpoch(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 871), clock); err != nil {
		t.Fatal(err)
	}
	h, clock := fundedHandoffRequest(t, d)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if r, err := s.HandoffHub(ctx, h, clock); err != nil || r.Remaining[d.ID] != 768 {
				t.Errorf("concurrent transfer %+v %v", r, err)
			}
		})
	}
	wg.Wait()
	completeFundedHandoff(t, s, clock)
	if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 872), clock); err != nil {
		t.Fatal(err)
	}
	h2 := h
	h2.ID = shareID(873)
	h2.FromEpoch = 2
	h2.ToEpoch = 3
	h2.Heartbeat = h.Heartbeat.Add(5 * time.Second)
	h2.FundedSuccessors = append([]FundedHandoffSuccessor(nil), h.FundedSuccessors...)
	h2.FundedSuccessors[0].ShareReferences = append([]ShareReference(nil), h.FundedSuccessors[0].ShareReferences...)
	for i := range h2.FundedSuccessors[0].ShareReferences {
		h2.FundedSuccessors[0].ShareReferences[i].SignerEpoch++
		h2.FundedSuccessors[0].ShareReferences[i].Revision++
	}
	clock.Earliest, clock.Latest = h2.Heartbeat, h2.Heartbeat.Add(time.Second)
	if r, err := s.HandoffHub(ctx, h2, clock); err != nil || r.Remaining[d.ID] != 512 {
		t.Fatalf("second transfer %+v %v", r, err)
	}
	completeFundedHandoff(t, s, clock)
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.IssueHubAuthorization(ctx, issuerRequest(d, 874), clock); err != nil {
		t.Fatal(err)
	}
	if _, err = other.HandoffHub(ctx, h, IssuerTime{}); err != nil {
		t.Fatal("old handoff replay", err)
	}
	b, err := other.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 768 || b.Remaining != 256 {
		t.Fatalf("refilled %+v %v", b, err)
	}
}

func TestFundedHandoffAcrossSQLiteConnectionsCannotFork(t *testing.T) {
	s, d, _, path := renewalIssuerFixture(t)
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	h, clock := fundedHandoffRequest(t, d)
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, db := range []*Store{s, other} {
		candidate := h
		candidate.ID = shareID(890 + i)
		wg.Go(func() {
			<-start
			if _, err := db.HandoffHub(context.Background(), candidate, clock); err == nil {
				successes.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	var transfers, roots int
	if err = s.db.QueryRow(`SELECT count(*) FROM hub_transfers`).Scan(&transfers); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM share_source_anchors`).Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if successes.Load() != 1 || transfers != 1 || roots != 6 {
		t.Fatalf("forked successes=%d transfers=%d roots=%d", successes.Load(), transfers, roots)
	}
}

func TestFundedHandoffRequiresEveryOpenDelegationOrExplicitRevocation(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	d2 := d
	d2.ID = shareID(900)
	d2.SourceID = shareID(901)
	d2.RuntimeSourceID = shareID(902)
	d2.ShareSourceID = shareID(903)
	d2.ShareReferences = append([]ShareReference(nil), d.ShareReferences...)
	for i, source := range []string{d2.SourceID, d2.RuntimeSourceID, d2.ShareSourceID} {
		d2.ShareReferences[i].ID = shareID(910 + i)
		d2.ShareReferences[i].SourceID = source
	}
	ctx := context.Background()
	if err := s.InstallFundedHubDelegation(ctx, shareID(799), d2, clock); err != nil {
		t.Fatal(err)
	}
	h, clock := fundedHandoffRequest(t, d)
	if _, err := s.HandoffHub(ctx, h, clock); err == nil {
		t.Fatal("omitted live funded allocation")
	}
	h.RevokeDelegations = []string{d2.ID}
	r, err := s.HandoffHub(ctx, h, clock)
	if err != nil || len(r.Remaining) != 1 || r.Remaining[d.ID] != 1024 {
		t.Fatalf("revocation %+v %v", r, err)
	}
	if _, ok := r.Remaining[d2.ID]; ok {
		t.Fatal("revoked R moved")
	}
	b, err := s.AuditHubDelegation(ctx, d2.ID)
	if err != nil || b.Remaining != 1024 {
		t.Fatalf("revoked frozen funds changed %+v %v", b, err)
	}
}
