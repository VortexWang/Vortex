package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func shareID(n int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", n) }

func shareFixture(t *testing.T) (*Store, ShareReference, AuthorizationUpdate, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 11, 6, 4, 0, 0, 0, time.UTC)
	r := ShareReference{ID: shareID(1), SourceID: shareID(2), IssuerID: shareID(3), NodeID: shareID(4), SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: "rule", RuleRevision: 1, PathGeneration: 1, RateBPS: 10000, Connections: 10, NotBefore: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour)}
	u := AuthorizationUpdate{IssuerID: r.IssuerID, NodeID: r.NodeID, BootID: shareID(5), UpdateID: shareID(6), Payload: []byte("initial share fixture"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1, ShareReferences: []ShareReference{r}}
	return s, r, u, path
}

func shareUpdate(u AuthorizationUpdate, id int, refs ...ShareReference) AuthorizationUpdate {
	u.UpdateID = shareID(id)
	u.ShareReferences = refs
	u.Payload, _ = json.Marshal(struct {
		ID   string
		Refs []ShareReference
	}{u.UpdateID, refs})
	return u
}

func shareGrant(r ShareReference, u AuthorizationUpdate) AuthorizationRecord {
	return AuthorizationRecord{IssuerID: u.IssuerID, AuthorizationID: shareID(20), ParentAuthorizationID: shareID(21), DelegationID: shareID(21), ShareReferenceID: r.ID, SourceID: r.SourceID, Kind: "credit_grant", RuleID: r.RuleID, RuleRevision: r.RuleRevision, PathGeneration: r.PathGeneration, SignerEpoch: r.SignerEpoch, BootID: u.BootID, IssuedAt: r.NotBefore.Format(time.RFC3339Nano), NotBefore: r.NotBefore.Format(time.RFC3339Nano), ExpiresAt: r.NotBefore.Add(time.Minute).Format(time.RFC3339Nano), HardDeadline: r.HardDeadline.Format(time.RFC3339Nano), BillingSnapshotID: shareID(22), AmountBytes: 256}
}

func TestShareRootMustMatchSeparatelyProvisionedAnchor(t *testing.T) {
	s, r, u, _ := shareFixture(t)
	ctx := context.Background()
	a := shareGrant(r, u)
	if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{a}, nil); err == nil {
		t.Fatal("wire-supplied root became its own authority")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM share_references`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected root committed: %d %v", count, err)
	}
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
		t.Fatal(err)
	}
	wrong := r
	wrong.RateBPS++
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 30, wrong), nil, nil); err == nil {
		t.Fatal("different root bytes accepted")
	}
	if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{a}, nil); err != nil {
		t.Fatal(err)
	}
	use := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, AuthorizationID: a.AuthorizationID, Kind: a.Kind, RuleID: a.RuleID, RuleRevision: 1, PathGeneration: 1, BillingSnapshotID: a.BillingSnapshotID, Earliest: r.NotBefore, Latest: r.NotBefore, TrustedTime: true}
	got, err := s.CheckStoredAuthorization(ctx, use)
	if err != nil || got.ShareRevision != 1 {
		t.Fatalf("anchored grant: %+v %v", got, err)
	}
	// A newer independently provisioned root fences new admission; established
	// sessions retain the historical root and their original funded boundary.
	newRoot := r
	newRoot.Revision = 2
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), newRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); err == nil {
		t.Fatal("old root admitted a new OPEN")
	}
	use.ExistingSession = true
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("old valid session cannot drain: %v", err)
	}
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
		t.Fatal(err)
	}
	use.ExistingSession = false
	if _, err := s.CheckStoredAuthorization(ctx, use); err == nil {
		t.Fatal("old anchor replay rolled back current root")
	}
}

func TestShareChainsRejectMissingCycleIssuerAndOverAllocation(t *testing.T) {
	for _, name := range []string{"missing-parent", "cycle", "issuer", "overflow-capacity", "duplicate", "wrong-source", "outside-time", "too-deep"} {
		t.Run(name, func(t *testing.T) {
			s, r, u, _ := shareFixture(t)
			ctx := context.Background()
			if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
				t.Fatal(err)
			}
			child := r
			child.ID = shareID(10)
			child.ParentID = r.ID
			child.ParentRevision = r.Revision
			child.RateBPS = 6000
			child.Connections = 6
			refs := []ShareReference{r, child}
			switch name {
			case "missing-parent":
				refs = refs[1:]
			case "cycle":
				refs[0].ParentID = child.ID
				refs[0].ParentRevision = child.Revision
			case "issuer":
				refs[1].IssuerID = shareID(91)
			case "overflow-capacity":
				other := child
				other.ID = shareID(11)
				refs = append(refs, other)
			case "duplicate":
				refs = append(refs, child)
			case "wrong-source":
				refs[1].SourceID = shareID(91)
			case "outside-time":
				refs[1].ExpiresAt = r.ExpiresAt.Add(time.Second)
				refs[1].HardDeadline = refs[1].ExpiresAt
			case "too-deep":
				last := r
				refs = []ShareReference{r}
				for i := range 32 {
					c := r
					c.ID = shareID(100 + i)
					c.ParentID = last.ID
					c.ParentRevision = 1
					refs = append(refs, c)
					last = c
				}
			}
			if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 30, refs...), nil, nil); err == nil {
				t.Fatal("invalid chain accepted")
			}
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM share_references`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial chain committed: %d %v", count, err)
			}
		})
	}
}

func TestShareSiblingCASAndReopenRetainOriginalAuthority(t *testing.T) {
	s, r, u, path := shareFixture(t)
	ctx := context.Background()
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 30, r), nil, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := r
			c.ID = shareID(40 + i)
			c.ParentID = r.ID
			c.ParentRevision = 1
			c.RateBPS = 6000
			c.Connections = 6
			err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 50+i, c), nil, nil)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			}
		}(i)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("sibling capacity allocated %d times", success)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM share_reference_heads`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("reopened heads=%d %v", count, err)
	}
	changed := r
	changed.RateBPS--
	if err := reopened.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), changed); err == nil {
		t.Fatal("same anchor revision changed after restart")
	}
	if err := reopened.InstallShareSourceAnchor(ctx, u.NodeID, shareID(91), r); err == nil {
		t.Fatal("different backend took anchor ownership")
	}
}

func TestShareWriteFailureRollsBackReferencesAndGrant(t *testing.T) {
	s, r, u, _ := shareFixture(t)
	ctx := context.Background()
	if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_share BEFORE INSERT ON authorization_records BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{shareGrant(r, u)}, nil); err == nil {
		t.Fatal("failed grant commit accepted")
	}
	for _, table := range []string{"authorization_records", "authorization_updates", "share_references", "share_reference_heads"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s: %d %v", table, count, err)
		}
	}
}
