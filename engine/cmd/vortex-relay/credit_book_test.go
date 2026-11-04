package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"vortex/engine/runtime/accounting"
	"vortex/engine/runtime/config"
)

func creditBookConfig() config.Config {
	c := config.Config{Version: 1, NodeID: "00000000-0000-0000-0000-000000000001", Role: config.RoleSource, Certificate: "cert.pem", PrivateKey: "key.pem", TrustBundle: "ca.pem", Listen: "127.0.0.1:9443", Peer: "127.0.0.1:9444", ServiceListen: "127.0.0.1:8080", RuleID: "rule-1", RuleRevision: 1, PathGeneration: 1, ExecutionPoint: true, EvidenceFile: "events.jsonl", TransportMode: "mux"}
	version := func(id string, coefficient uint64) accounting.CreditVersion {
		return accounting.CreditVersion{Binding: accounting.Binding{RuleID: c.RuleID, ExecutionNode: c.NodeID, PathGeneration: 1, PolicyRevision: 1, CoefficientMilli: coefficient}, Credit: accounting.CreditConfig{SnapshotID: id, Budget: 128 << 10, InitialBlock: 64 << 10, MaxBlock: 64 << 10, GrantTarget: 64 << 10, RenewPercent: 30}}
	}
	c.SourceCreditBook = &accounting.BookConfig{FundingBytes: 512 << 10, Active: "old", SelectionRevision: 1, Snapshots: map[string]accounting.CreditVersion{"old": version("old", 1000), "new": version("new", 3000)}}
	return c
}

func TestApplyCreditSelectionAllowsOnlyPinnedRevisionChange(t *testing.T) {
	initial := creditBookConfig()
	book, err := accounting.NewCreditBook(*initial.SourceCreditBook)
	if err != nil {
		t.Fatal(err)
	}
	next := initial
	nextBook := *initial.SourceCreditBook
	nextBook.Active, nextBook.SelectionRevision = "new", 2
	next.SourceCreditBook = &nextBook
	if err := applyCreditSelection(initial, next, book); err != nil {
		t.Fatal(err)
	}
	if state := book.State(); state.Active != "new" || state.SelectionRevision != 2 {
		t.Fatal(state)
	}
	stale := next
	staleBook := *next.SourceCreditBook
	staleBook.Active, staleBook.SelectionRevision = "old", 2
	stale.SourceCreditBook = &staleBook
	if err := applyCreditSelection(initial, stale, book); err == nil {
		t.Fatal("same revision switched snapshot")
	}
	mutated := next
	mutatedBook := *next.SourceCreditBook
	mutatedBook.FundingBytes++
	mutated.SourceCreditBook = &mutatedBook
	if err := applyCreditSelection(initial, mutated, book); err == nil {
		t.Fatal("reload changed pinned funding")
	}
}

func TestCreditBookReloadSignalAppliesSelection(t *testing.T) {
	initial := creditBookConfig()
	book, err := accounting.NewCreditBook(*initial.SourceCreditBook)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "source.json")
	write := func(c config.Config) {
		t.Helper()
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	next := initial
	nextBook := *initial.SourceCreditBook
	nextBook.Active, nextBook.SelectionRevision = "new", 2
	next.SourceCreditBook = &nextBook
	write(next)
	stop := watchCreditBook(t.Context(), path, initial, book)
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for book.State().Active != "new" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if state := book.State(); state.Active != "new" || state.SelectionRevision != 2 {
		t.Fatal(state)
	}
}
