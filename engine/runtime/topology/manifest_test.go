package topology

import (
	"testing"

	"vortex/engine/runtime/config"
)

func TestManifestRequiresThreeRelays(t *testing.T) {
	m := Manifest{Version: 1, RuleID: "r", RuleRevision: 1, PathGeneration: 1, Nodes: []Node{
		{Name: "source", ID: "00000000-0000-0000-0000-000000000001", Role: config.RoleSource, Listen: "127.0.0.1:1", Peer: "127.0.0.1:2", ServiceListen: "127.0.0.1:3", QualificationOnly: true, EvidenceFile: "source.jsonl"},
		{Name: "relay-1", ID: "00000000-0000-0000-0000-000000000002", Role: config.RoleRelay, Listen: "127.0.0.1:2", Peer: "127.0.0.1:4"},
		{Name: "relay-2", ID: "00000000-0000-0000-0000-000000000003", Role: config.RoleRelay, Listen: "127.0.0.1:4", Peer: "127.0.0.1:5"},
		{Name: "relay-3", ID: "00000000-0000-0000-0000-000000000004", Role: config.RoleRelay, Listen: "127.0.0.1:5", Peer: "127.0.0.1:6"},
		{Name: "destination", ID: "00000000-0000-0000-0000-000000000005", Role: config.RoleDestination, Listen: "127.0.0.1:6", Target: "127.0.0.1:7"},
	}}
	if digest, err := m.Digest(); err != nil || len(digest) != 64 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}
