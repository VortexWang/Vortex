package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"vortex/contracts"
)

func nodeBootRecoveryFixture(t *testing.T) (contracts.NodeBootRecovery, IssuerTime) {
	t.Helper()
	at := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	c := contracts.NodeBootRecovery{Version: 1, ID: shareID(810), BackendID: shareID(811), TenantID: shareID(812), NodeID: shareID(813), FromBootID: shareID(814), ToBootID: shareID(815), MinimumFencing: 12, SafeAfter: at.Add(10 * time.Minute), IssuedAt: at.Add(10*time.Minute + time.Second), ExpiresAt: at.Add(11 * time.Minute), AuthorityProofSHA256: strings.Repeat("a", 64), Previous: []contracts.ShareHandoff{}}
	for i, kind := range []string{"rate", "connections"} {
		generation := int64(7)
		amount, burst := int64(600000), int64(75000)
		if i == 1 {
			generation = 3
			amount, burst = 4, 0
		}
		c.Previous = append(c.Previous, contracts.ShareHandoff{TenantID: c.TenantID, ID: shareID(820 + i), NodeID: c.NodeID, BootID: c.FromBootID, Kind: kind, SignerID: shareID(830), SnapshotID: shareID(831), RuleID: shareID(832), Generation: generation, Predecessor: generation - 1, SignerEpoch: 7, PathGeneration: 3, FencingGeneration: int64(11 - i), RuleRevision: 1, Amount: amount, BurstBytes: burst, ClosureDigest: strings.Repeat("b", 64), IssuedAt: at, OwnershipUntil: at.Add(5 * time.Minute), PolicyUntil: at.Add(6 * time.Minute), AutonomyUntil: at.Add(10 * time.Minute)})
	}
	return c, IssuerTime{Earliest: c.IssuedAt, Latest: c.IssuedAt, Trusted: true}
}
func seedBootRecoveryHeads(t *testing.T, s *Store, c contracts.NodeBootRecovery) {
	t.Helper()
	for _, head := range c.Previous {
		for n := int64(1); n <= head.Generation; n++ {
			p := head
			p.ID = shareID(900 + int(n)*2)
			if head.Kind == "connections" {
				p.ID = shareID(901 + int(n)*2)
			}
			p.Generation = n
			p.Predecessor = n - 1
			if n == 1 {
				p.ClosureDigest = ""
			}
			if n == head.Generation {
				p = head
			}
			raw, _ := json.Marshal(p)
			if _, e := s.ApplyShareHandoff(context.Background(), c.NodeID, c.FromBootID, c.BackendID, "cas", raw, IssuerTime{Earliest: head.IssuedAt, Latest: head.IssuedAt, Trusted: true}); e != nil {
				t.Fatal(e)
			}
		}
	}
}
func applyBootRecovery(t *testing.T, s *Store, c contracts.NodeBootRecovery, now IssuerTime) (contracts.NodeBootRecoveryReceipt, error) {
	t.Helper()
	raw, _ := json.Marshal(c)
	return s.RetireNodeBootShares(context.Background(), c.NodeID, c.ToBootID, c.BackendID, raw, now)
}

func TestNodeBootRetirementFULLRollbackIndependentHeadsReopenAndNextCAS(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh-disk", true: "retained-disk"}[existing], func(t *testing.T) {
			c, now := nodeBootRecoveryFixture(t)
			path := filepath.Join(t.TempDir(), "node.db")
			s, e := Open(path, "agent")
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if existing {
				seedBootRecoveryHeads(t, s, c)
			}
			var receiptsBefore int
			if e = s.db.QueryRow(`SELECT count(*) FROM node_share_commands`).Scan(&receiptsBefore); e != nil {
				t.Fatal(e)
			}
			if _, e = s.db.Exec(`CREATE TRIGGER fail_retirement BEFORE INSERT ON node_boot_share_recoveries BEGIN SELECT RAISE(ABORT,'injected FULL retirement failure'); END;`); e != nil {
				t.Fatal(e)
			}
			if _, e = applyBootRecovery(t, s, c, now); e == nil {
				t.Fatal("write failure produced retirement receipt")
			}
			var n int
			if e = s.db.QueryRow(`SELECT count(*) FROM node_share_heads WHERE boot_id=?`, c.ToBootID).Scan(&n); e != nil || n != 0 {
				t.Fatal("partial new boot heads", n, e)
			}
			if _, e = s.db.Exec(`DROP TRIGGER fail_retirement`); e != nil {
				t.Fatal(e)
			}
			first, e := applyBootRecovery(t, s, c, now)
			if e != nil || len(first.Retired) != 2 || first.Retired[0].Generation != 7 || first.Retired[1].Generation != 3 {
				t.Fatal(first, e)
			}
			tx, e := s.db.Begin()
			if e != nil {
				t.Fatal(e)
			}
			_, bound, e := nodeShareTx(context.Background(), tx, AuthorizationUse{NodeID: c.NodeID, BootID: c.ToBootID, RuleID: c.Previous[0].RuleID, Earliest: now.Earliest, Latest: now.Latest, TrustedTime: true}, "rate")
			tx.Rollback()
			if !bound || e == nil {
				t.Fatal("retirement activated capacity", bound, e)
			}
			if e = s.db.QueryRow(`SELECT count(*) FROM node_share_commands`).Scan(&n); e != nil || n != receiptsBefore {
				t.Fatal("manufactured old execution receipt", n, e)
			}
			reopened, e := Open(path, "agent")
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			afterExpiry := now
			afterExpiry.Earliest = c.ExpiresAt.Add(time.Hour)
			afterExpiry.Latest = afterExpiry.Earliest
			again, e := applyBootRecovery(t, reopened, c, afterExpiry)
			x, _ := json.Marshal(first)
			y, _ := json.Marshal(again)
			if e != nil || !bytes.Equal(x, y) {
				t.Fatal("retirement exact replay changed", e)
			}
			for i, p := range c.Previous {
				next := p
				next.ID = shareID(980 + i)
				next.BootID = c.ToBootID
				next.Generation++
				next.Predecessor = p.Generation
				next.FencingGeneration = c.MinimumFencing
				next.SignerEpoch++
				next.IssuedAt = now.Latest
				next.OwnershipUntil = now.Latest.Add(time.Minute)
				next.PolicyUntil = next.OwnershipUntil
				next.AutonomyUntil = next.OwnershipUntil
				raw, _ := json.Marshal(next)
				r, e := reopened.ApplyShareHandoff(context.Background(), c.NodeID, c.ToBootID, c.BackendID, "cas", raw, now)
				if e != nil || r.State != "enforced" || r.ActiveGeneration != p.Generation+1 {
					t.Fatal("next original-floor CAS", r, e)
				}
			}
			if _, e = applyBootRecovery(t, reopened, c, now); e != nil {
				t.Fatal("late replay after next CAS", e)
			}
			var generation int64
			if e = reopened.db.QueryRow(`SELECT generation FROM node_share_heads WHERE kind='rate'`).Scan(&generation); e != nil || generation != 8 {
				t.Fatal("late replay lowered head", generation, e)
			}
			raw, _ := json.Marshal(c.Previous[0])
			if _, e = reopened.ApplyShareHandoff(context.Background(), c.NodeID, c.ToBootID, c.BackendID, "cas", raw, now); !errors.Is(e, ErrAuthorizationDenied) {
				t.Fatal("old boot resurrected", e)
			}
		})
	}
}

func TestNodeBootRetirementRejectsClockScopeAndConflictingOriginals(t *testing.T) {
	c, now := nodeBootRecoveryFixture(t)
	for _, name := range []string{"wrong-backend", "wrong-node", "wrong-boot", "uncertain", "lower-bound", "expired", "changed-original", "existing-new-head", "wrong-floor"} {
		t.Run(name, func(t *testing.T) {
			s, e := Open(filepath.Join(t.TempDir(), "node.db"), "agent")
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			seedBootRecoveryHeads(t, s, c)
			input := c
			input.Previous = append([]contracts.ShareHandoff(nil), c.Previous...)
			clock := now
			node, boot, backend := c.NodeID, c.ToBootID, c.BackendID
			switch name {
			case "wrong-backend":
				backend = shareID(999)
			case "wrong-node":
				node = shareID(999)
			case "wrong-boot":
				boot = shareID(999)
			case "uncertain":
				clock.Trusted = false
			case "lower-bound":
				clock.Earliest = c.SafeAfter.Add(-time.Second)
			case "expired":
				clock.Latest = c.ExpiresAt
			case "changed-original":
				input.Previous[1].Amount++
			case "wrong-floor":
				input.MinimumFencing = 11
			case "existing-new-head":
				if _, e = s.db.Exec(`UPDATE node_share_heads SET generation=generation+1 WHERE kind='connections'`); e != nil {
					t.Fatal(e)
				}
			}
			raw, _ := json.Marshal(input)
			if _, e = s.RetireNodeBootShares(context.Background(), node, boot, backend, raw, clock); e == nil {
				t.Fatal("invalid retirement accepted")
			}
			var n int
			if e = s.db.QueryRow(`SELECT count(*) FROM node_share_heads WHERE boot_id=?`, c.ToBootID).Scan(&n); e != nil || n != 0 {
				t.Fatal("partial invalid retirement", n, e)
			}
		})
	}
}

func TestNodeBootRetirementConcurrentDifferentBootsCannotSplitHeads(t *testing.T) {
	c, now := nodeBootRecoveryFixture(t)
	path := filepath.Join(t.TempDir(), "node.db")
	a, e := Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	seedBootRecoveryHeads(t, a, c)
	b, e := Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, s := range []*Store{a, b} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			<-start
			input := c
			if i == 1 {
				input.ID = shareID(996)
				input.ToBootID = shareID(997)
			}
			raw, _ := json.Marshal(input)
			_, e := s.RetireNodeBootShares(context.Background(), input.NodeID, input.ToBootID, input.BackendID, raw, now)
			results <- e
		}(i, s)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("competing boots acquired heads", success)
	}
	var distinct int
	if e = a.db.QueryRow(`SELECT count(DISTINCT boot_id) FROM node_share_heads`).Scan(&distinct); e != nil || distinct != 1 {
		t.Fatal("split-kind boot ownership", distinct, e)
	}
}

func TestNodeBootRetirementRequiresLaterLocalGrantBoundaryAndRetainsDebt(t *testing.T) {
	s, r, u, _ := shareFixture(t)
	ctx := context.Background()
	c, now := nodeBootRecoveryFixture(t)
	c.NodeID = u.NodeID
	c.FromBootID = u.BootID
	r.RuleID = c.Previous[0].RuleID
	r.NotBefore = c.Previous[0].IssuedAt
	r.ExpiresAt = r.NotBefore.Add(time.Hour)
	r.HardDeadline = r.ExpiresAt
	u.ShareReferences = []ShareReference{r}
	for i := range c.Previous {
		c.Previous[i].NodeID = c.NodeID
		c.Previous[i].BootID = c.FromBootID
	}
	if e := s.InstallShareSourceAnchor(ctx, u.NodeID, c.BackendID, r); e != nil {
		t.Fatal(e)
	}
	grant := shareGrant(r, u)
	if e := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); e != nil {
		t.Fatal(e)
	}
	seedBootRecoveryHeads(t, s, c)
	if _, e := applyBootRecovery(t, s, c, now); !errors.Is(e, ErrAuthorityExpired) {
		t.Fatal("unacknowledged local later hard deadline ignored", e)
	}
	c.SafeAfter = r.HardDeadline
	c.IssuedAt = c.SafeAfter.Add(time.Second)
	c.ExpiresAt = c.IssuedAt.Add(time.Minute)
	now.Earliest = c.IssuedAt
	now.Latest = c.IssuedAt
	if _, e := applyBootRecovery(t, s, c, now); e != nil {
		t.Fatal(e)
	}
	if remaining, e := s.RemainingAuthorizationBytes(ctx, c.NodeID, grant.AuthorizationID); e != nil || remaining != 256 {
		t.Fatal("cold retirement returned original credit", remaining, e)
	}
	var oldBoot string
	var hard string
	if e := s.db.QueryRow(`SELECT boot_id,hard_deadline FROM authorization_records WHERE authorization_id=?`, grant.AuthorizationID).Scan(&oldBoot, &hard); e != nil || oldBoot != c.FromBootID || hard != grant.HardDeadline {
		t.Fatal("cold retirement rewrote original grant", oldBoot, hard, e)
	}
}

func TestNodeBootRetirementReplayRejectsChangedCommandAndCorruptReceipt(t *testing.T) {
	c, now := nodeBootRecoveryFixture(t)
	s, e := Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = applyBootRecovery(t, s, c, now); e != nil {
		t.Fatal(e)
	}
	changed := c
	changed.AuthorityProofSHA256 = strings.Repeat("c", 64)
	if _, e = applyBootRecovery(t, s, changed, now); !errors.Is(e, ErrAuthorizationUpdateConflict) {
		t.Fatal("changed platform proof reused recovery ID", e)
	}
	if _, e = s.db.Exec(`UPDATE node_boot_share_recoveries SET receipt='{}'`); e == nil {
		t.Fatal("mutable FULL recovery receipt")
	}
	// Simulate stored-byte corruption; normal SQL cannot bypass immutability.
	if _, e = s.db.Exec(`DROP TRIGGER node_boot_recovery_no_update; UPDATE node_boot_share_recoveries SET receipt='{}'`); e != nil {
		t.Fatal(e)
	}
	if _, e = applyBootRecovery(t, s, c, now); !errors.Is(e, ErrAuthorizationUpdateConflict) {
		t.Fatal("corrupt receipt replayed", e)
	}
}
