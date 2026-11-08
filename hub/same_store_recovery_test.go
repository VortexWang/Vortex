package hub

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
)

func sameStoreCommandFixture() SameStoreRecoveryCommand {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	id, h, backend, node, delegation := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	roots := []store.ShareReference{}
	for i := 0; i < 3; i++ {
		roots = append(roots, store.ShareReference{ID: uuid.NewString(), SourceID: uuid.NewString(), IssuerID: h, NodeID: node, SignerEpoch: 8, Generation: 7, Revision: 4, RuleID: uuid.NewString(), RuleRevision: 2, PathGeneration: 4, RateBPS: 4096, Connections: 3, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour)})
	}
	return SameStoreRecoveryCommand{Version: 1, ID: id, BackendID: backend, HubID: h, FromBootID: uuid.NewString(), ToBootID: uuid.NewString(), IssuedAt: now, ExpiresAt: now.Add(4 * time.Minute), Handoff: store.HandoffRequest{ID: id, HubID: h, BackendID: backend, FromEpoch: 7, ToEpoch: 8, Heartbeat: now, RevokeDelegations: []string{}, FundedSuccessors: []store.FundedHandoffSuccessor{{DelegationID: delegation, AllocationSHA256: strings.Repeat("a", 64), ShareReferences: roots}}}, Allocations: []SameStoreAllocationLink{{AllocationID: uuid.NewString(), PayloadSHA256: strings.Repeat("b", 64), DelegationID: delegation, DelegationSHA256: strings.Repeat("a", 64)}}}
}

func TestSameStoreRecoveryCanonicalAndExplicitScope(t *testing.T) {
	c := sameStoreCommandFixture()
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeSameStoreRecovery(raw); e != nil {
		t.Fatal(e)
	}
	// A process reconnect may have the same boot; admission still requires the
	// explicit epoch transition and complete prefix, never a connection change.
	c.ToBootID = c.FromBootID
	raw, _ = json.Marshal(c)
	if _, e = DecodeSameStoreRecovery(raw); e != nil {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{nil, append(raw, ' '), append(raw, raw...), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(raw, []byte(`"version":1`), []byte(`"unknown":1,"version":1`), 1), bytes.Repeat([]byte("x"), (1<<20)+1)} {
		if _, e = DecodeSameStoreRecovery(bad); e == nil {
			t.Fatal("noncanonical command accepted")
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SameStoreRecoveryCommand)
	}{
		{"hub", func(c *SameStoreRecoveryCommand) { c.Handoff.HubID = uuid.NewString() }},
		{"backend", func(c *SameStoreRecoveryCommand) { c.Handoff.BackendID = uuid.NewString() }},
		{"id", func(c *SameStoreRecoveryCommand) { c.Handoff.ID = uuid.NewString() }},
		{"boot", func(c *SameStoreRecoveryCommand) { c.ToBootID = "invalid" }},
		{"overflow", func(c *SameStoreRecoveryCommand) {
			c.Handoff.FromEpoch = math.MaxInt64
			c.Handoff.ToEpoch = uint64(math.MaxInt64) + 1
		}},
		{"gap", func(c *SameStoreRecoveryCommand) { c.Handoff.ToEpoch++ }},
		{"ttl", func(c *SameStoreRecoveryCommand) { c.ExpiresAt = c.IssuedAt.Add(6 * time.Minute) }},
		{"heartbeat", func(c *SameStoreRecoveryCommand) { c.Handoff.Heartbeat = c.IssuedAt.Add(-time.Second) }},
		{"revocation", func(c *SameStoreRecoveryCommand) { c.Handoff.RevokeDelegations = []string{uuid.NewString()} }},
		{"missing", func(c *SameStoreRecoveryCommand) { c.Allocations = nil }},
		{"mismatch", func(c *SameStoreRecoveryCommand) {
			c.Handoff.FundedSuccessors[0].AllocationSHA256 = strings.Repeat("b", 64)
		}},
		{"root-epoch", func(c *SameStoreRecoveryCommand) { c.Handoff.FundedSuccessors[0].ShareReferences[0].SignerEpoch = 7 }},
		{"root-issuer", func(c *SameStoreRecoveryCommand) {
			c.Handoff.FundedSuccessors[0].ShareReferences[0].IssuerID = uuid.NewString()
		}},
		{"duplicate-source", func(c *SameStoreRecoveryCommand) {
			c.Handoff.FundedSuccessors[0].ShareReferences[1].SourceID = c.Handoff.FundedSuccessors[0].ShareReferences[0].SourceID
		}},
		{"root-expiry", func(c *SameStoreRecoveryCommand) {
			c.Handoff.FundedSuccessors[0].ShareReferences[0].ExpiresAt = c.IssuedAt
		}},
		{"short-hash", func(c *SameStoreRecoveryCommand) { c.Allocations[0].PayloadSHA256 = "00" }},
		{"duplicate-delegation", func(c *SameStoreRecoveryCommand) {
			a := c.Allocations[0]
			a.AllocationID = "ffffffff-ffff-ffff-ffff-ffffffffffff"
			c.Allocations = append(c.Allocations, a)
			c.Handoff.FundedSuccessors = append(c.Handoff.FundedSuccessors, c.Handoff.FundedSuccessors[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sameStoreCommandFixture()
			tc.mutate(&c)
			raw, _ := json.Marshal(c)
			if _, e := DecodeSameStoreRecovery(raw); e == nil {
				t.Fatal("unsafe command accepted")
			}
		})
	}
}
