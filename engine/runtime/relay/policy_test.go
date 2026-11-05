package relay

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

func TestStaticPolicyRequiresExplicitQualificationAuthorization(t *testing.T) {
	local, peer, next := "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000003"
	cfg := config.Config{NodeID: local, Role: config.RoleRelay, PeerNodeID: peer, NextHopNodeID: next, RuleID: "rule", RuleRevision: 1, PathGeneration: 1}
	policy, err := StaticPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rule := sha256.Sum256([]byte(cfg.RuleID))
	request := vlink.OpenRequest{RuleID: [16]byte(rule[:16]), RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{127, 0, 0, 1}}
	peerID, err := vlink.ParseNodeID(peer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Authorize(context.Background(), peerID, request); err == nil {
		t.Fatal("default static policy authorized an OPEN without runtime/share/billing authority")
	} else {
		var rejection *vlink.OpenRejection
		if !errors.As(err, &rejection) || rejection.Code != vlink.OpenUnauthorized {
			t.Fatalf("unexpected default OPEN rejection: %v", err)
		}
	}

	cfg.QualificationOnly = true
	policy, err = StaticPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Authorize(context.Background(), peerID, request); err != nil {
		t.Fatalf("explicit qualification fixture was rejected: %v", err)
	}
	cfg.QualificationOnly = false
	cfg.RuntimeAuthority = true
	policy, err = StaticPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Authorize(context.Background(), peerID, request); err != nil {
		t.Fatalf("explicit pinned runtime authority was rejected: %v", err)
	}
}
