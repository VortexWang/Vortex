package contracts

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestShareRenewalClosureCanonicalBindingAndBoundedPages(t *testing.T) {
	c := ShareRenewalClosure{Version: 1, ID: uuid.NewString(), BackendID: uuid.NewString(), TenantID: uuid.NewString(), HandoffID: uuid.NewString(), HubID: uuid.NewString(), HubBootID: uuid.NewString(), NodeID: uuid.NewString(), BootID: uuid.NewString(), Kind: "rate", Generation: 3, SuccessorGeneration: 4, SignerEpoch: 7, Part: 1, Parts: 1, DelegationIDs: []string{}}
	raw, _ := json.Marshal(c)
	if _, e := DecodeShareRenewalClosure(raw); e != nil {
		t.Fatal(e)
	}
	for _, raw := range [][]byte{append([]byte(" "), raw...), append(raw, []byte(" ")...), []byte(`{}`)} {
		if _, e := DecodeShareRenewalClosure(raw); e == nil {
			t.Fatal("noncanonical accepted")
		}
	}
	for _, change := range []func(*ShareRenewalClosure){func(c *ShareRenewalClosure) { c.DelegationIDs = nil }, func(c *ShareRenewalClosure) { c.Parts = 2 }, func(c *ShareRenewalClosure) { c.SuccessorGeneration = 5 }, func(c *ShareRenewalClosure) { c.Kind = "runtime" }, func(c *ShareRenewalClosure) { id := uuid.NewString(); c.DelegationIDs = []string{id, id} }, func(c *ShareRenewalClosure) { c.DelegationIDs = make([]string, 65) }} {
		bad := c
		change(&bad)
		b, _ := json.Marshal(bad)
		if _, e := DecodeShareRenewalClosure(b); e == nil {
			t.Fatal("invalid scope/page accepted", string(b))
		}
	}
	r := ShareRenewalClosureReceipt{Version: 1, ID: c.ID, CommandDigest: SharePayloadDigest(raw)}
	b, _ := json.Marshal(r)
	if _, e := DecodeShareRenewalClosureReceipt(b); e != nil {
		t.Fatal(e)
	}
	r.CommandDigest = "ABC"
	b, _ = json.Marshal(r)
	if _, e := DecodeShareRenewalClosureReceipt(b); e == nil {
		t.Fatal("bad digest")
	}
}
