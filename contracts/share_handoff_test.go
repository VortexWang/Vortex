package contracts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestShareHandoffStrictDecisionAndObservation(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	at := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	c := ShareHandoff{TenantID: id, ID: id, NodeID: id, BootID: id, SignerID: id, SnapshotID: id, RuleID: id, Kind: "rate", Generation: 1, SignerEpoch: 7, PathGeneration: 2, FencingGeneration: 3, RuleRevision: 1, Amount: 600000, BurstBytes: 75000, IssuedAt: at, OwnershipUntil: at.Add(time.Minute), PolicyUntil: at.Add(time.Minute), AutonomyUntil: at.Add(time.Minute)}
	raw, _ := json.Marshal(c)
	if _, e := DecodeShareHandoff(raw); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*ShareHandoff){
		func(c *ShareHandoff) { c.Generation = 2 },
		func(c *ShareHandoff) { c.BurstBytes++ },
		func(c *ShareHandoff) { c.Kind = "connections" },
		func(c *ShareHandoff) { c.SignerEpoch = 0 },
		func(c *ShareHandoff) { c.OwnershipUntil = c.IssuedAt },
		func(c *ShareHandoff) { c.NodeID = "invalid" },
		func(c *ShareHandoff) { c.ClosureDigest = strings.Repeat("a", 64) },
	} {
		bad := c
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if _, e := DecodeShareHandoff(b); e == nil {
			t.Fatal("bad decision accepted", string(b))
		}
	}
	for _, b := range [][]byte{append(raw, []byte("{}")...), append([]byte(`{"unknown":1,`), raw[1:]...), make([]byte, 16385)} {
		if _, e := DecodeShareHandoff(b); e == nil {
			t.Fatal("unknown/trailing/oversized payload accepted")
		}
	}
	r := ShareObservation{ReceiptID: strings.Repeat("a", 32), HandoffID: id, NodeID: id, BootID: id, Kind: "rate", CommandDigest: SharePayloadDigest(raw), State: "enforced", Generation: 1, ActiveGeneration: 1, MinimumGeneration: 1, FencingGeneration: 3}
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*ShareObservation){func(r *ShareObservation) { r.State = "draining" }, func(r *ShareObservation) { r.ReceiptID = strings.Repeat("G", 32) }, func(r *ShareObservation) { r.CommandDigest = "bad" }, func(r *ShareObservation) { r.OpenConnections = -1 }, func(r *ShareObservation) { r.Generation = 0 }} {
		bad := r
		mutate(&bad)
		if e := bad.Validate(); e == nil {
			t.Fatal("bad observation accepted", bad)
		}
	}
}
