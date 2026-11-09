package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func recoveryAdmissionFixture(t *testing.T) (*Store, HubDelegation, HubRecoveryAdmission, IssuerTime, string) {
	t.Helper()
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.IssueHubAuthorization(ctx, issuerRequest(d, 851), clock); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 40; i++ {
		if e := s.PersistUsageLedger(ctx, UsageBatch{NodeID: d.NodeID, BootID: d.BootID, BatchID: fmt.Sprint("before/", i), SnapshotID: d.BillingSnapshotID, Payload: []byte(fmt.Sprint("before/", i))}); e != nil {
			t.Fatal(e)
		}
	}
	h, clock := fundedHandoffRequest(t, d)
	r, e := s.HandoffHub(ctx, h, clock)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.LedgerPrefix(ctx, r.SealedSequence, r.SealedSequence+1, 32)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	proof := sha256.Sum256([]byte("explicit authenticated PG admission fixture"))
	when := clock.Latest.Add(time.Minute)
	p := HubRecoveryAdmission{Version: 1, RecoveryID: h.ID, BackendID: h.BackendID, HubID: h.HubID, BootID: shareID(890), FromEpoch: h.FromEpoch, ToEpoch: h.ToEpoch, SealedSequence: r.SealedSequence, AdmissionSHA256: hex.EncodeToString(proof[:]), ManifestSHA256: hex.EncodeToString(rows[0].Digest), PrefixSHA256: r.FundedInventory.PrefixSHA256, ObservedAt: when, AutonomyDeadline: d.AutonomyDeadline.Add(time.Hour)}
	return s, d, p, IssuerTime{Earliest: when, Latest: when.Add(time.Second), Trusted: true}, path
}

func TestRecoveryAdmissionFULLAtomicFinitePrefixConcurrentReopenNoFunds(t *testing.T) {
	s, d, p, clock, path := recoveryAdmissionFixture(t)
	ctx := context.Background()
	raw, _ := json.Marshal(p)
	before, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PersistUsageLedger(ctx, UsageBatch{NodeID: d.NodeID, BootID: d.BootID, BatchID: "later-tail", SnapshotID: d.BillingSnapshotID, Payload: []byte("unconfirmed later usage tail")}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER fail_admission_full BEFORE INSERT ON hub_recovery_admissions BEGIN SELECT RAISE(ABORT,'injected admission FULL failure'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyHubRecoveryAdmission(ctx, p.BackendID, p.HubID, p.BootID, raw, clock); e == nil {
		t.Fatal("admission escaped FULL failure")
	}
	rows, e := s.PendingLedger(ctx, 1000)
	mode, _ := s.AgentState(ctx, "authority_mode")
	if e != nil || len(rows) != 43 || mode != AutonomyRecovering {
		t.Fatal("partial prefix ACK/mode escaped FULL rollback", len(rows), mode, e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_admission_full`); e != nil {
		t.Fatal(e)
	}
	other, e := Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		writer := []*Store{s, other}[i%2]
		wg.Go(func() {
			result, e := writer.ApplyHubRecoveryAdmission(ctx, p.BackendID, p.HubID, p.BootID, raw, clock)
			if e != nil || !bytes.Equal(result, raw) {
				t.Error("concurrent FULL admission", e)
			}
		})
	}
	wg.Wait()
	rows, e = other.PendingLedger(ctx, 1000)
	if e != nil || len(rows) != 1 || rows[0].ID != d.NodeID+"/later-tail" {
		t.Fatal("admission ACKed later usage tail", rows, e)
	}
	state, e := other.CurrentAutonomyStatus(ctx, clock.Latest, true)
	if e != nil || state.Mode != AutonomyOnline || !state.HardDeadline.Equal(d.AutonomyDeadline) || !state.BackendHeartbeat.Equal(p.ObservedAt) {
		t.Fatal("admission changed original deadline or failed fresh liveness", state, e)
	}
	after, e := other.AuditHubDelegation(ctx, d.ID)
	if e != nil || before != after {
		t.Fatal("admission changed financial R/debt", before, after, e)
	}
	p.ObservedAt = p.ObservedAt.Add(time.Hour)
	p.AutonomyDeadline = p.ObservedAt.Add(12 * time.Hour)
	retimed, _ := json.Marshal(p)
	if result, e := other.ApplyHubRecoveryAdmission(ctx, p.BackendID, p.HubID, p.BootID, retimed, IssuerTime{}); e != nil || !bytes.Equal(result, raw) {
		t.Fatal("replay retimed admission or required clock", e)
	}
	state, e = other.CurrentAutonomyStatus(ctx, clock.Latest, true)
	if e != nil || !state.BackendHeartbeat.Equal(clock.Earliest) || !state.HardDeadline.Equal(d.AutonomyDeadline) {
		t.Fatal("replay changed liveness/deadline", state, e)
	}
	if _, e = other.db.Exec(`DELETE FROM hub_recovery_admissions`); e == nil {
		t.Fatal("admission receipt mutable")
	}
}

func TestRecoveryAdmissionScopeClockPrefixAndClosedEpochNegatives(t *testing.T) {
	for _, name := range []string{"backend", "boot", "manifest", "prefix", "sequence", "epoch", "clock", "stale", "expiry", "closed", "hole", "rehashed-row", "ordinary-heartbeat"} {
		t.Run(name, func(t *testing.T) {
			s, _, p, clock, _ := recoveryAdmissionFixture(t)
			ctx := context.Background()
			switch name {
			case "backend":
				p.BackendID = shareID(891)
			case "boot":
				p.BootID = shareID(891)
			case "manifest":
				p.ManifestSHA256 = string(bytes.Repeat([]byte("0"), 64))
			case "prefix":
				p.PrefixSHA256 = string(bytes.Repeat([]byte("0"), 64))
			case "sequence":
				p.SealedSequence--
			case "epoch":
				p.FromEpoch++
				p.ToEpoch++
			case "clock":
				clock.Trusted = false
			case "stale":
				clock.Latest = p.ObservedAt.Add(5 * time.Second)
				clock.Earliest = clock.Latest
			case "expiry":
				clock.Earliest, clock.Latest = p.AutonomyDeadline, p.AutonomyDeadline
			case "closed":
				if _, e := s.db.Exec(`UPDATE issuer_epochs SET state='closed'`); e != nil {
					t.Fatal(e)
				}
			case "hole":
				if _, e := s.db.Exec(`DELETE FROM hub_ledger WHERE sequence=2`); e != nil {
					t.Fatal(e)
				}
			case "rehashed-row":
				payload := []byte("replacement with a valid new row hash")
				h := sha256.Sum256(payload)
				if _, e := s.db.Exec(`UPDATE hub_ledger SET payload=?,payload_sha256=? WHERE sequence=2`, payload, h[:]); e != nil {
					t.Fatal(e)
				}
			case "ordinary-heartbeat":
				if e := s.RecordBackendHeartbeat(ctx, p.ObservedAt, p.AutonomyDeadline); e == nil {
					t.Fatal("ordinary heartbeat bypassed30s recovery guard")
				}
			}
			raw, _ := json.Marshal(p)
			if _, e := s.ApplyHubRecoveryAdmission(ctx, shareID(799), p.HubID, shareID(890), raw, clock); e == nil {
				t.Fatal("invalid admission accepted", name)
			}
			var confirmed, completed, admitted int
			if e := s.db.QueryRow(`SELECT (SELECT count(*) FROM hub_ledger WHERE confirmed=1),(SELECT sum(completed) FROM hub_recoveries),(SELECT count(*) FROM hub_recovery_admissions)`).Scan(&confirmed, &completed, &admitted); e != nil || confirmed != 0 || completed != 0 || admitted != 0 {
				t.Fatal("invalid admission ACKed prefix or completed recovery", confirmed, completed, admitted, e)
			}
		})
	}
}
