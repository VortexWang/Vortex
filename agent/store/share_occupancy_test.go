package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"
)

func TestConfigShareOccupancyRetainsUnknownAndRequiresProvenRelease(t *testing.T) {
	s, r, u, _ := shareFixture(t)
	ctx := context.Background()
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
		t.Fatal(err)
	}
	child := r
	child.ID = shareID(80)
	child.ParentID = r.ID
	child.ParentRevision = 1
	child.RateBPS = 6000
	child.Connections = 6
	grant := shareGrant(child, u)
	grant.AmountBytes = 65536
	runtime := grant
	runtime.AuthorizationID = shareID(81)
	runtime.Kind = "runtime_lease"
	runtime.AmountBytes = 0
	runtime.BillingSnapshotID = ""
	share := runtime
	share.AuthorizationID = shareID(82)
	share.Kind = "share_lease"
	share.RateBPS = 6000
	share.Connections = 6
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 83, r, child), []AuthorizationRecord{grant, runtime, share}, nil); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	raw := []byte("config")
	hash := sha256.Sum256(raw)
	if _, err := s.SaveConfigRequest(ctx, ConfigResult{TransactionID: id, Version: 1, FencingGeneration: 1, Payload: raw, PayloadSHA256: hash[:]}); err != nil {
		t.Fatal(err)
	}
	scope := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, RuleID: r.RuleID, RuleRevision: 1, PathGeneration: 1, FencingGeneration: 1, Earliest: r.NotBefore.Add(time.Second), Latest: r.NotBefore.Add(time.Second), TrustedTime: true}
	if err := s.HoldRuntimeShares(ctx, id, scope); err != nil {
		t.Fatal(err)
	}
	if err := s.ObserveActivation(ctx, id, 1, "activation-unknown"); err != nil {
		t.Fatal(err)
	}
	next := child
	next.Revision = 2
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 84, next), nil, nil); err == nil {
		t.Fatal("unknown old occupancy released by new revision")
	}
	if err := s.ObserveActivation(ctx, id, 1, "missing"); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 85, next), nil, nil); err == nil {
		t.Fatal("missing executor treated as proof of release")
	}
	if err := s.ObserveActivation(ctx, id, 1, "aborted"); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 86, next), nil, nil); err != nil {
		t.Fatalf("proven release did not free old reference: %v", err)
	}
	if _, err := s.OpenRuntimeSession(ctx, scope, "stale", "stale-flow"); err == nil {
		t.Fatal("stale share admitted new OPEN")
	}
	if err := s.CheckRuntimeReady(ctx, scope); err == nil {
		t.Fatal("stale share allowed Commit")
	}
}

func TestShareRuntimeAdmissionRejectsHostileInstalledChain(t *testing.T) {
	// Corruption tests exercise checks at admission, independently from the
	// update decoder's earlier checks. No corrupt row becomes a trusted fixture.
	for _, mode := range []string{"missing-parent", "wrong-issuer", "cycle", "stale-revision", "over-capacity"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, _, u, future, _ := policyFixture(t)
			ctx := context.Background()
			if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			var id string
			if err := s.db.QueryRow(`SELECT share_id,payload FROM share_references ORDER BY rowid LIMIT 1`).Scan(&id, &raw); err != nil {
				t.Fatal(err)
			}
			var r ShareReference
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing-parent":
				r.ParentID = shareID(990)
				r.ParentRevision = 1
			case "wrong-issuer":
				r.IssuerID = shareID(991)
			case "cycle":
				r.ParentID = r.ID
				r.ParentRevision = 1
			case "stale-revision":
				if _, err := s.db.Exec(`UPDATE share_reference_heads SET revision='2' WHERE share_id=?`, id); err != nil {
					t.Fatal(err)
				}
			case "over-capacity":
				r.RateBPS = 1
				r.Connections = 1
			}
			raw, _ = json.Marshal(r)
			hash := sha256.Sum256(raw)
			if mode != "stale-revision" {
				if _, err := s.db.Exec(`UPDATE share_references SET payload=?,payload_sha256=? WHERE share_id=?`, raw, hash[:], id); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CheckRuntimeReady(ctx, scope); err == nil {
				t.Fatal("hostile chain allowed Commit")
			}
			if _, err := s.OpenRuntimeSession(ctx, scope, "hostile", "hostile"); err == nil {
				t.Fatal("hostile chain allowed OPEN")
			}
		})
	}
}

func TestPlatformCommitNeedsDurableNodeAggregatesBeyondChildLeases(t *testing.T) {
	s, root, update, _ := shareFixture(t)
	ctx := context.Background()
	root.RuleID = shareID(992)
	if e := s.InstallShareSourceAnchor(ctx, update.NodeID, shareID(90), root); e != nil {
		t.Fatal(e)
	}
	credit := shareGrant(root, update)
	lease := credit
	lease.AuthorizationID, lease.Kind, lease.BillingSnapshotID, lease.AmountBytes = shareID(993), "runtime_lease", "", 0
	share := lease
	share.AuthorizationID, share.Kind, share.RateBPS, share.Connections = shareID(994), "share_lease", root.RateBPS, root.Connections
	if e := s.PersistAuthorizationUpdate(ctx, shareUpdate(update, 995, root), []AuthorizationRecord{credit, lease, share}, nil); e != nil {
		t.Fatal(e)
	}
	scope := AuthorizationUse{NodeID: update.NodeID, BootID: update.BootID, RuleID: root.RuleID, RuleRevision: 1, PathGeneration: 1, FencingGeneration: 1, Earliest: root.NotBefore, Latest: root.NotBefore, TrustedTime: true}
	if e := s.CheckRuntimeReady(ctx, scope); e != nil {
		t.Fatal("fixture matching child lease", e)
	}
	if e := s.BindPlatformShareRule(ctx, shareID(991), scope.NodeID, scope.RuleID); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckRuntimeReady(ctx, scope); e == nil {
		t.Fatal("child leases bypassed missing Node bootstrap CAS")
	}
}
