package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDemandDenialMigratesAndPreservesPendingIdentity(t *testing.T) {
	_, d, _, _ := issuerFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	scope := DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: d.SignerEpoch, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration}
	r, err := s.PrepareDemand(ctx, scope, 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild the old v12 shape to exercise upgrading a real pending row.
	if _, err := s.db.Exec(`DROP TABLE authorization_demands; CREATE TABLE authorization_demands(request_id TEXT PRIMARY KEY NOT NULL,scope BLOB NOT NULL,request BLOB NOT NULL,state TEXT NOT NULL CHECK(state IN ('pending','applied')),grant_id TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	key, _ := json.Marshal(scope)
	raw, _ := json.Marshal(r)
	if _, err := s.db.Exec(`INSERT INTO authorization_demands VALUES(?,?,?,'pending','')`, r.ID, key, raw); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pending, err := s.PendingDemands(ctx, d.NodeID, d.BootID, d.HubID, "")
	if err != nil || len(pending) != 1 || pending[0] != r {
		t.Fatalf("migration lost pending: %+v %v", pending, err)
	}
	for _, identity := range []struct{ node, boot, hub string }{{d.SourceID, d.BootID, d.HubID}, {d.NodeID, d.SourceID, d.HubID}, {d.NodeID, d.BootID, d.SourceID}} {
		if err := s.DenyDemand(ctx, identity.node, identity.boot, identity.hub, r.ID, "expired"); err == nil {
			t.Fatal("wrong identity resolved demand")
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_demand_denial BEFORE UPDATE ON authorization_demands BEGIN SELECT RAISE(ABORT,'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.DenyDemand(ctx, d.NodeID, d.BootID, d.HubID, r.ID, "expired"); err == nil {
		t.Fatal("write failure acknowledged denial")
	}
	retry, err := s.PrepareDemand(ctx, scope, 512, time.Minute)
	if err != nil || retry != r {
		t.Fatalf("write failure replaced request: %+v %v", retry, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_demand_denial`); err != nil {
		t.Fatal(err)
	}
	if err := s.DenyDemand(ctx, d.NodeID, d.BootID, d.HubID, r.ID, "expired"); err != nil {
		t.Fatal(err)
	}
	next, err := s.PrepareDemand(ctx, scope, 128, time.Minute)
	if err != nil || next.ID == r.ID {
		t.Fatalf("new request %+v %v", next, err)
	}
	if err := s.DenyDemand(ctx, d.NodeID, d.BootID, d.HubID, r.ID, "invalid_scope"); err == nil {
		t.Fatal("changed denial accepted")
	}
	if err := s.CompleteDemand(ctx, r.ID, d.SourceID); err == nil {
		t.Fatal("denied request became issued")
	}
}
