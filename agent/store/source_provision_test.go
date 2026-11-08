package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSourceProvisionFULLRollbackReplayAndNoRuntimeAuthority(t *testing.T) {
	s, e := Open(t.TempDir()+"/agent.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	node, boot, backend := shareID(801), shareID(802), shareID(803)
	p := SourceProvision{Version: 1, ID: shareID(804), NodeID: node, BootID: boot, BackendID: backend, IssuedAt: now}
	for i := 0; i < 3; i++ {
		p.Roots = append(p.Roots, ShareReference{ID: shareID(810 + i), SourceID: shareID(820 + i), NodeID: node, IssuerID: shareID(805), SignerEpoch: 7, Generation: 1, Revision: 1, RuleID: shareID(806), RuleRevision: 1, PathGeneration: 1, RateBPS: 1000, Connections: 1, NotBefore: now, ExpiresAt: now.Add(time.Minute), HardDeadline: now.Add(time.Minute)})
	}
	raw, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER fail_second_root BEFORE INSERT ON share_source_anchors WHEN new.source_id='` + p.Roots[1].SourceID + `' BEGIN SELECT RAISE(ABORT,'injected source disk failure'); END`); e != nil {
		t.Fatal(e)
	}
	clock := IssuerTime{Earliest: now, Latest: now, Trusted: true}
	ctx := context.Background()
	if _, e = s.ApplySourceProvision(ctx, node, boot, backend, raw, clock); e == nil {
		t.Fatal("partial source provisioning acknowledged")
	}
	for _, table := range []string{"share_source_anchors", "share_source_heads", "node_source_provisions"} {
		var n int
		if e = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("partial roots survived", table, n, e)
		}
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_second_root`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER fail_source_receipt BEFORE INSERT ON node_source_provisions BEGIN SELECT RAISE(ABORT,'injected FULL receipt failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplySourceProvision(ctx, node, boot, backend, raw, clock); e == nil {
		t.Fatal("receipt write failure acknowledged")
	}
	for _, table := range []string{"share_source_anchors", "share_source_heads", "node_source_provisions"} {
		var n int
		if e = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("receipt failure left roots", table, n, e)
		}
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_source_receipt`); e != nil {
		t.Fatal(e)
	}
	var first SourceProvisionReceipt
	for i := 0; i < 2; i++ {
		r, e := s.ApplySourceProvision(ctx, node, boot, backend, raw, clock)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = r
		} else if r != first {
			t.Fatal("replay receipt changed")
		}
	}
	for _, table := range []string{"authorization_records", "usage_reservations", "runtime_sessions"} {
		var n int
		if e = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("roots created executable or financial rights", table, n, e)
		}
	}
	changed := p
	changed.Roots = append([]ShareReference(nil), p.Roots...)
	changed.Roots[1].ID = shareID(860)
	changed.Roots[1].SourceID = shareID(861)
	changedRaw, _ := json.Marshal(changed)
	if _, e = s.ApplySourceProvision(ctx, node, boot, backend, changedRaw, clock); e == nil {
		t.Fatal("same provision ID rebound")
	}
	if _, e = s.ApplySourceProvision(ctx, node, boot, backend, raw, IssuerTime{}); e != nil {
		t.Fatal("durable no-op receipt requires new authority", e)
	}
	if _, e = s.ApplySourceProvision(ctx, node, shareID(850), backend, raw, clock); e == nil {
		t.Fatal("wrong boot accepted")
	}
	if _, e = s.ApplySourceProvision(ctx, node, boot, shareID(851), raw, clock); e == nil {
		t.Fatal("wrong backend accepted")
	}
	bad := p
	bad.Roots = append([]ShareReference(nil), p.Roots...)
	bad.Roots[1].NodeID = shareID(852)
	badRaw, _ := json.Marshal(bad)
	if _, e = s.ApplySourceProvision(ctx, node, boot, backend, badRaw, clock); e == nil {
		t.Fatal("other node root accepted")
	}
}
