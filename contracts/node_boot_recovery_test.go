package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vortex/contracts"
)

func TestNodeBootRecoveryCanonicalScopeDeadlinesAndIndependentHeads(t *testing.T) {
	id := func(s string) string { return "550e8400-e29b-41d4-a716-" + s }
	at := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	c := contracts.NodeBootRecovery{Version: 1, ID: id("000000000001"), BackendID: id("000000000002"), TenantID: id("000000000003"), NodeID: id("000000000004"), FromBootID: id("000000000005"), ToBootID: id("000000000006"), MinimumFencing: 12, SafeAfter: at.Add(time.Hour), IssuedAt: at.Add(time.Hour), ExpiresAt: at.Add(time.Hour + time.Minute), AuthorityProofSHA256: strings.Repeat("a", 64)}
	for i, kind := range []string{"rate", "connections"} {
		g := int64(7 - i*4)
		c.Previous = append(c.Previous, contracts.ShareHandoff{TenantID: c.TenantID, ID: id("00000000000" + string(rune('7'+i))), NodeID: c.NodeID, BootID: c.FromBootID, Kind: kind, SignerID: c.BackendID, SnapshotID: c.TenantID, RuleID: c.NodeID, Generation: g, Predecessor: g - 1, SignerEpoch: 3, PathGeneration: 1, FencingGeneration: 10, RuleRevision: 1, Amount: 1, ClosureDigest: strings.Repeat("b", 64), IssuedAt: at, OwnershipUntil: c.SafeAfter, PolicyUntil: c.SafeAfter, AutonomyUntil: c.SafeAfter})
	}
	raw, _ := json.Marshal(c)
	decoded, e := contracts.DecodeNodeBootRecovery(raw)
	if e != nil || decoded.Previous[0].Generation != 7 || decoded.Previous[1].Generation != 3 {
		t.Fatal(decoded, e)
	}
	for _, payload := range [][]byte{append(append([]byte(nil), raw...), []byte(` {}`)...), []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1)), []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"unexpected":true`, 1))} {
		if _, e = contracts.DecodeNodeBootRecovery(payload); e == nil {
			t.Fatal("noncanonical or unknown payload accepted")
		}
	}
	for _, change := range []func(*contracts.NodeBootRecovery){func(c *contracts.NodeBootRecovery) { c.ToBootID = c.FromBootID }, func(c *contracts.NodeBootRecovery) { c.MinimumFencing = 10 }, func(c *contracts.NodeBootRecovery) { c.SafeAfter = c.SafeAfter.Add(-time.Second) }, func(c *contracts.NodeBootRecovery) { c.ExpiresAt = c.IssuedAt.Add(6 * time.Minute) }, func(c *contracts.NodeBootRecovery) { c.Previous[1].Kind = "rate" }} {
		bad := c
		bad.Previous = append([]contracts.ShareHandoff(nil), c.Previous...)
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid recovery validated")
		}
	}
}
