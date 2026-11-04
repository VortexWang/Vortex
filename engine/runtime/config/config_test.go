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
	for _, mutate := range []func(*Config){func(v *Config) { v.Role = RoleRelay }, func(v *Config) { v.ExecutionPoint = false }, func(v *Config) { v.EvidenceFile = "" }, func(v *Config) { v.UDPServiceListen = "127.0.0.1:1235" }, func(v *Config) { v.Routes = map[string]Route{"a": {}} }} {
		bad := c
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
