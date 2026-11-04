package link

import (
	"context"
	"testing"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

func routeConfig() config.Config {
	c := config.Config{Version: 1, NodeID: "00000000-0000-0000-0000-000000000001", Role: config.RoleRelay, Certificate: "c", PrivateKey: "k", TrustBundle: "ca", Listen: "127.0.0.1:1", Peer: "127.0.0.1:2", RuleID: "r", RuleRevision: 1, PathGeneration: 1, TransportMode: vlink.ModeMux, ActiveRoute: "a"}
	c.Routes = map[string]config.Route{"a": {Peer: "127.0.0.1:2", PeerNodeID: "00000000-0000-0000-0000-000000000002", NextHopNodeID: "00000000-0000-0000-0000-000000000003"}}
	return c
}
func TestRouteReloadRejectsUnauthorizedMutationsBeforeDial(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
	}{
		{"unlisted", func(c *config.Config) { c.ActiveRoute = "b" }},
		{"target", func(c *config.Config) { c.Target = "127.0.0.1:99" }},
		{"candidate", func(c *config.Config) { v := c.Routes["a"]; v.Peer = "127.0.0.1:99"; c.Routes["a"] = v }},
		{"identity", func(c *config.Config) { c.NodeID = "00000000-0000-0000-0000-000000000099" }},
		{"same-generation", func(c *config.Config) { c.PathGeneration = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := routeConfig()
			r, _ := NewRoutes(context.Background(), base, &Supervisor{})
			defer r.Close()
			next := routeConfig()
			next.PathGeneration = 2
			tc.change(&next)
			if err := r.Apply(context.Background(), next); err == nil {
				t.Fatal("unsafe reload accepted")
			}
			if r.Snapshot().PathGeneration != 1 {
				t.Fatal("failed reload changed active generation")
			}
		})
	}
}
func TestOldGenerationAdmissionAndLeaseDrain(t *testing.T) {
	c := routeConfig()
	r, _ := NewRoutes(context.Background(), c, &Supervisor{})
	defer r.Close()
	if _, _, _, err := r.Acquire(context.Background(), 0, vlink.StreamTCP); err == nil {
		t.Fatal("old OPEN accepted")
	}
	old := &routeEntry{cfg: c, owner: &Supervisor{}, refs: 1, draining: true}
	r.entries[1] = old
	r.release(old)
	if len(r.entries) != 0 {
		t.Fatal("drained generation retained")
	}
}
