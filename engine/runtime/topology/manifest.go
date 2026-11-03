package topology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"vortex/engine/runtime/config"
)

type Node struct {
	Name           string      `json:"name"`
	ID             string      `json:"node_id"`
	Role           config.Role `json:"role"`
	Listen         string      `json:"listen"`
	Peer           string      `json:"peer,omitempty"`
	PeerNodeID     string      `json:"peer_node_id,omitempty"`
	NextHopNodeID  string      `json:"next_hop_node_id,omitempty"`
	ServiceListen  string      `json:"service_listen,omitempty"`
	Target         string      `json:"target,omitempty"`
	ExecutionPoint bool        `json:"execution_point,omitempty"`
}

type Manifest struct {
	Version        uint64 `json:"version"`
	RuleID         string `json:"rule_id"`
	RuleRevision   uint64 `json:"rule_revision"`
	PathGeneration uint64 `json:"path_generation"`
	Nodes          []Node `json:"nodes"`
}

func (m Manifest) Validate() error {
	if m.Version == 0 || m.RuleID == "" || m.RuleRevision == 0 || m.PathGeneration == 0 {
		return errors.New("topology: version, rule and generation fields are required")
	}
	if len(m.Nodes) != 5 {
		return fmt.Errorf("topology: exactly five nodes are required, got %d", len(m.Nodes))
	}
	seen := make(map[string]struct{}, len(m.Nodes))
	roles := map[config.Role]int{}
	for _, node := range m.Nodes {
		if node.Name == "" || node.ID == "" || node.Listen == "" {
			return errors.New("topology: node name, id and listen are required")
		}
		candidate := config.Config{Version: 1, NodeID: node.ID, Role: node.Role, Certificate: "c", PrivateKey: "k", TrustBundle: "ca", Listen: node.Listen, Peer: node.Peer, ServiceListen: node.ServiceListen, Target: node.Target, RuleID: m.RuleID, RuleRevision: m.RuleRevision, PathGeneration: m.PathGeneration}
		if err := candidate.Validate(); err != nil && node.Role != config.RoleDestination {
			return fmt.Errorf("topology: node %s: %w", node.Name, err)
		}
		if _, duplicate := seen[node.ID]; duplicate {
			return fmt.Errorf("topology: duplicate node id %s", node.ID)
		}
		seen[node.ID] = struct{}{}
		roles[node.Role]++
	}
	if roles[config.RoleSource] != 1 || roles[config.RoleRelay] != 3 || roles[config.RoleDestination] != 1 {
		return fmt.Errorf("topology: roles must be one source, three relays and one destination: %+v", roles)
	}
	return nil
}

func (m Manifest) Digest() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
