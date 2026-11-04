package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	runtimeaccounting "vortex/engine/runtime/accounting"
)

func validJSON() string {
	return `{"version":1,"node_id":"00000000-0000-0000-0000-000000000001","role":"relay","certificate":"cert.pem","private_key":"key.pem","trust_bundle":"ca.pem","listen":"127.0.0.1:9443","peer":"127.0.0.1:9444","rule_id":"rule-1","rule_revision":1,"path_generation":1,"hop_index":1}`
}

func TestLoadFileRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(strings.TrimSuffix(validJSON(), "}")+`,"extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestValidateRoleAndRequiredPeer(t *testing.T) {
	cfg := Config{Version: 1, NodeID: "00000000-0000-0000-0000-000000000001", Role: RoleSource, Certificate: "c", PrivateKey: "k", TrustBundle: "ca", Listen: "127.0.0.1:1", RuleID: "r", RuleRevision: 1, PathGeneration: 1}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected source peer requirement")
	}
}

func TestUDPServiceListenIsOptionalAndSourceOnly(t *testing.T) {
	cfg := Config{Version: 1, NodeID: "00000000-0000-0000-0000-000000000001", Role: RoleSource, Certificate: "c", PrivateKey: "k", TrustBundle: "ca", Listen: "127.0.0.1:1", Peer: "127.0.0.1:2", ServiceListen: "127.0.0.1:3", RuleID: "r", RuleRevision: 1, PathGeneration: 1}
	cfg.QualificationOnly, cfg.EvidenceFile = true, "evidence.jsonl"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("TCP-only source config rejected: %v", err)
	}
	cfg.UDPServiceListen = "not-a-host-port"
	if err := cfg.Validate(); err == nil {
		t.Fatal("malformed UDP service address accepted")
	}
	cfg.UDPServiceListen = "127.0.0.1:4"
	cfg.Role = RoleRelay
	if err := cfg.Validate(); err == nil {
		t.Fatal("relay config accepted source UDP listener")
	}
}

func TestSourceRequiresFundedGateOutsideQualificationMode(t *testing.T) {
	cfg := Config{Version: 1, NodeID: "00000000-0000-0000-0000-000000000001", Role: RoleSource, Certificate: "c", PrivateKey: "k", TrustBundle: "ca", Listen: "127.0.0.1:1", Peer: "127.0.0.1:2", ServiceListen: "127.0.0.1:3", RuleID: "r", RuleRevision: 1, PathGeneration: 1}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "funded source credit gate") {
		t.Fatalf("ungated source config should fail closed, got %v", err)
	}
	cfg.QualificationOnly = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires evidence_file") {
		t.Fatalf("qualification-only mode without evidence file should fail, got %v", err)
	}
	cfg.EvidenceFile = "events.jsonl"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("explicit bounded qualification mode rejected: %v", err)
	}
	cfg.QualificationOnly = false
	cfg.SourceCredit = &runtimeaccounting.CreditConfig{SnapshotID: "empty", Budget: 0, InitialBlock: 1, MaxBlock: 65536, GrantTarget: 65536, RenewPercent: 30}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "budget must be funded") {
		t.Fatalf("zero-budget source Gate should not count as funded, got %v", err)
	}
}

func TestSourceCreditRejectsUngatedCombinations(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(validJSON()), &c); err != nil {
		t.Fatal(err)
	}
	c.Role = RoleSource
	c.ExecutionPoint = true
	c.ServiceListen = "127.0.0.1:1234"
	c.EvidenceFile = "evidence.jsonl"
	c.TransportMode = "mux"
	c.SourceCredit = &runtimeaccounting.CreditConfig{SnapshotID: "snapshot", Budget: 4096, InitialBlock: 256, MaxBlock: 65536, GrantTarget: 65536, RenewPercent: 30}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Config){func(v *Config) { v.Role = RoleRelay }, func(v *Config) { v.ExecutionPoint = false }, func(v *Config) { v.EvidenceFile = "" }, func(v *Config) { v.Routes = map[string]Route{"a": {}} }, func(v *Config) { v.SourceCredit.Budget = 0 }} {
		bad := c
		credit := *c.SourceCredit
		bad.SourceCredit = &credit
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestSourceCreditBookPinsExecutionAndBillingBindings(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(validJSON()), &c); err != nil {
		t.Fatal(err)
	}
	c.Role, c.ExecutionPoint = RoleSource, true
	c.ServiceListen, c.EvidenceFile, c.TransportMode = "127.0.0.1:1234", "evidence.jsonl", "mux"
	version := func(id string, coefficient uint64) runtimeaccounting.CreditVersion {
		return runtimeaccounting.CreditVersion{
			Binding: runtimeaccounting.Binding{RuleID: "rule-1", ExecutionNode: c.NodeID, PathGeneration: 1, PolicyRevision: 1, CoefficientMilli: coefficient},
			Credit:  runtimeaccounting.CreditConfig{SnapshotID: id, Budget: 128 << 10, InitialBlock: 64 << 10, MaxBlock: 64 << 10, GrantTarget: 64 << 10, RenewPercent: 30},
		}
	}
	c.SourceCreditBook = &runtimeaccounting.BookConfig{FundingBytes: 512 << 10, Active: "old", SelectionRevision: 1, Snapshots: map[string]runtimeaccounting.CreditVersion{"old": version("old", 1000), "new": version("new", 3000)}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Config){
		func(v *Config) { v.SourceCredit = &runtimeaccounting.CreditConfig{} },
		func(v *Config) { v.Routes = map[string]Route{"r": {}} },
		func(v *Config) {
			x := v.SourceCreditBook.Snapshots["new"]
			x.Binding.RuleID = "other"
			v.SourceCreditBook.Snapshots["new"] = x
		},
		func(v *Config) {
			x := v.SourceCreditBook.Snapshots["old"]
			x.Credit.Budget = 0
			v.SourceCreditBook.Snapshots["old"] = x
		},
		func(v *Config) {
			x := v.SourceCreditBook.Snapshots["new"]
			x.Binding.ExecutionNode = "00000000-0000-0000-0000-000000000002"
			v.SourceCreditBook.Snapshots["new"] = x
		},
		func(v *Config) {
			x := v.SourceCreditBook.Snapshots["new"]
			x.Binding.PathGeneration++
			v.SourceCreditBook.Snapshots["new"] = x
		},
	} {
		bad := c
		book := *c.SourceCreditBook
		versions := make(map[string]runtimeaccounting.CreditVersion, len(book.Snapshots))
		for id, version := range book.Snapshots {
			versions[id] = version
		}
		book.Snapshots = versions
		bad.SourceCreditBook = &book
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted mutated book config: %+v", bad.SourceCreditBook)
		}
	}
}
