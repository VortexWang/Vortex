package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
)

func TestRuleConfigScopeIndependentAndRestartHolds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, e := Open(path, "node")
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte("immutable")
	sum := sha256.Sum256(raw)
	request := func(id string, revision, generation uint64) ConfigResult {
		return ConfigResult{TransactionID: id, Version: revision, FencingGeneration: generation, Payload: raw, PayloadSHA256: sum[:]}
	}
	a := request("11111111111111111111111111111111", 7, 9)
	if _, e = s.SaveRuleConfigRequest(ctx, "rule-a", a); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveConfigResult(ctx, a.TransactionID, 7, "prepared", ""); e != nil {
		t.Fatal(e)
	}
	b := request("22222222222222222222222222222222", 1, 1)
	if _, e = s.SaveRuleConfigRequest(ctx, "rule-b", b); e != nil {
		t.Fatal("another rule blocked by prepared rule", e)
	}
	if e = s.ObserveActivation(ctx, b.TransactionID, 1, "active"); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM agent_state WHERE key='applied_version'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("rule advanced legacy version", e, count)
	}
	if _, e = s.SaveRuleConfigRequest(ctx, "rule-a", request("33333333333333333333333333333333", 8, 10)); !errors.Is(e, ErrConfigUnresolved) {
		t.Fatal("unknown/prepared hold bypassed", e)
	}
	s.Close()
	s, e = Open(path, "node")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	aRecord, e := s.GetConfigResult(ctx, a.TransactionID)
	if e != nil || aRecord.State != "prepared" {
		t.Fatal("restart changed Prepare", e, aRecord.State)
	}
	if _, e = s.SaveRuleConfigRequest(ctx, "rule-b", a); !errors.Is(e, ErrConfigRequestConflict) {
		t.Fatal("transaction changed rule identity", e)
	}
}
