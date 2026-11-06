package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"vortex/agent"
	"vortex/agent/store"
)

// Local provisioning only: no authority is created by reading this file.
// Boot identity is supplied by the running Node, never reused from a file.
type demandFile struct {
	IntervalMillis uint64        `json:"interval_ms"`
	Demands        []demandEntry `json:"demands"`
}
type demandEntry struct {
	Scope      store.DemandScope        `json:"scope"`
	Policy     agent.DemandPolicyConfig `json:"policy"`
	TTLSeconds uint64                   `json:"ttl_seconds"`
}

var demandUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func loadDemandConfig(path, nodeID, bootID, hubID string) ([]agent.AdaptiveDemandConfig, time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > 1<<20 {
		return nil, 0, fmt.Errorf("credit demand configuration exceeds 1 MiB")
	}
	return parseDemandConfig(raw, nodeID, bootID, hubID)
}
func parseDemandConfig(raw []byte, nodeID, bootID, hubID string) ([]agent.AdaptiveDemandConfig, time.Duration, error) {
	var file demandFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, 0, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, 0, fmt.Errorf("credit demand configuration must contain one JSON object")
	}
	if file.IntervalMillis < 100 || file.IntervalMillis > 60000 || len(file.Demands) == 0 || len(file.Demands) > 64 {
		return nil, 0, fmt.Errorf("credit demand requires 1..64 scopes and interval_ms in 100..60000")
	}
	if !demandUUID.MatchString(nodeID) || !demandUUID.MatchString(bootID) || !demandUUID.MatchString(hubID) {
		return nil, 0, fmt.Errorf("credit demand requires UUID node, boot and Hub identities")
	}
	configs := make([]agent.AdaptiveDemandConfig, 0, len(file.Demands))
	seen := make(map[store.DemandScope]bool)
	for i, entry := range file.Demands {
		scope := entry.Scope
		if (scope.NodeID != "" && scope.NodeID != nodeID) || scope.BootID != "" || scope.HubID != hubID {
			return nil, 0, fmt.Errorf("credit demand %d: node/Hub mismatch or persisted boot identity", i)
		}
		for _, id := range []string{scope.DelegationID, scope.SourceID, scope.SnapshotID} {
			if !demandUUID.MatchString(id) {
				return nil, 0, fmt.Errorf("credit demand %d: invalid scope UUID", i)
			}
		}
		if scope.RuleID == "" || len(scope.RuleID) > 128 || scope.SignerEpoch == 0 || scope.RuleRevision == 0 || scope.PathGeneration == 0 || entry.TTLSeconds == 0 || entry.TTLSeconds > 300 {
			return nil, 0, fmt.Errorf("credit demand %d: incomplete scope or invalid TTL", i)
		}
		if _, err := agent.NewDemandPolicy(entry.Policy); err != nil {
			return nil, 0, fmt.Errorf("credit demand %d: %w", i, err)
		}
		scope.NodeID, scope.BootID = nodeID, bootID
		if seen[scope] {
			return nil, 0, fmt.Errorf("credit demand %d: duplicate scope", i)
		}
		seen[scope] = true
		configs = append(configs, agent.AdaptiveDemandConfig{Scope: scope, Policy: entry.Policy, TTL: time.Duration(entry.TTLSeconds) * time.Second})
	}
	return configs, time.Duration(file.IntervalMillis) * time.Millisecond, nil
}
