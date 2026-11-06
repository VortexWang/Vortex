package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent"
	"vortex/agent/store"
)

const testNode = "550e8400-e29b-41d4-a716-446655440001"
const testBoot = "550e8400-e29b-41d4-a716-446655440002"
const testHub = "550e8400-e29b-41d4-a716-446655440003"

func validDemandFile() demandFile {
	return demandFile{IntervalMillis: 1000, Demands: []demandEntry{{
		Scope:  store.DemandScope{HubID: testHub, DelegationID: "550e8400-e29b-41d4-a716-446655440004", SourceID: "550e8400-e29b-41d4-a716-446655440005", SnapshotID: "550e8400-e29b-41d4-a716-446655440006", RuleID: "rule", SignerEpoch: 1, RuleRevision: 2, PathGeneration: 3},
		Policy: agent.DemandPolicyConfig{Minimum: 8 << 20, MaxTarget: 64 << 20, TargetSeconds: 30, RenewPercent: 30}, TTLSeconds: 60,
	}}}
}
func TestDemandProvisioningBindsCurrentBoot(t *testing.T) {
	raw, _ := json.Marshal(validDemandFile())
	path := filepath.Join(t.TempDir(), "demand.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	configs, interval, err := loadDemandConfig(path, testNode, testBoot, testHub)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].Scope.NodeID != testNode || configs[0].Scope.BootID != testBoot || configs[0].TTL != time.Minute || interval != time.Second {
		t.Fatalf("bad binding: %+v %v", configs, interval)
	}
	reboot := "550e8400-e29b-41d4-a716-446655440007"
	other, _, err := loadDemandConfig(path, testNode, reboot, testHub)
	if err != nil || other[0].Scope.BootID != reboot {
		t.Fatalf("reboot binding: %+v %v", other, err)
	}
}
func TestDemandProvisioningRejectsInvalidOrAmbiguousScope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*demandFile)
	}{
		{"foreign node", func(f *demandFile) { f.Demands[0].Scope.NodeID = testHub }},
		{"stale boot", func(f *demandFile) { f.Demands[0].Scope.BootID = testBoot }},
		{"foreign Hub", func(f *demandFile) { f.Demands[0].Scope.HubID = testNode }},
		{"bad source", func(f *demandFile) { f.Demands[0].Scope.SourceID = "source" }},
		{"missing epoch", func(f *demandFile) { f.Demands[0].Scope.SignerEpoch = 0 }},
		{"missing rule", func(f *demandFile) { f.Demands[0].Scope.RuleID = "" }},
		{"overflow TTL", func(f *demandFile) { f.Demands[0].TTLSeconds = ^uint64(0) }},
		{"zero TTL", func(f *demandFile) { f.Demands[0].TTLSeconds = 0 }},
		{"invalid target", func(f *demandFile) { f.Demands[0].Policy.MaxTarget = 1 }},
		{"zero policy", func(f *demandFile) { f.Demands[0].Policy = agent.DemandPolicyConfig{} }},
		{"fast poll", func(f *demandFile) { f.IntervalMillis = 99 }},
		{"overflow interval", func(f *demandFile) { f.IntervalMillis = ^uint64(0) }},
		{"duplicate", func(f *demandFile) { f.Demands = append(f.Demands, f.Demands[0]) }},
		{"empty", func(f *demandFile) { f.Demands = nil }},
		{"too many", func(f *demandFile) {
			for len(f.Demands) < 65 {
				f.Demands = append(f.Demands, f.Demands[0])
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := validDemandFile()
			tt.mutate(&f)
			raw, _ := json.Marshal(f)
			if _, _, err := parseDemandConfig(raw, testNode, testBoot, testHub); err == nil {
				t.Fatal("accepted invalid provisioning")
			}
		})
	}
	raw, _ := json.Marshal(validDemandFile())
	for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte(` {}`)...), []byte(`{"interval_ms":1000,"demnads":[]}`), []byte(`null`)} {
		if _, _, err := parseDemandConfig(bad, testNode, testBoot, testHub); err == nil {
			t.Fatal("accepted malformed provisioning")
		}
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, make([]byte, (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadDemandConfig(path, testNode, testBoot, testHub); err == nil {
		t.Fatal("accepted oversized provisioning")
	}
}
