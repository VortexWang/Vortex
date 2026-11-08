package contracts

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCreditCloseCanonicalBoundsAndNoFinalOnUnsettled(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440001"
	c := CreditCloseCommand{Version: 1, ID: id, BackendID: id, HubID: id, NodeID: id, BootID: id, GrantID: id, Generation: 1, Original: []byte("original issue")}
	raw, _ := json.Marshal(c)
	if decoded, err := DecodeCreditCloseCommand(raw); err != nil || !bytes.Equal(decoded.Original, c.Original) {
		t.Fatalf("original command: %+v %v", decoded, err)
	}
	for _, bad := range [][]byte{append(raw, ' '), append([]byte(`{"extra":0,`), raw[1:]...), append([]byte(`{"version":1,`), raw[1:]...)} {
		if _, err := DecodeCreditCloseCommand(bad); err == nil {
			t.Fatal("noncanonical/unknown/duplicate command accepted")
		}
	}
	c.Original = make([]byte, (512<<10)+1)
	if c.Validate() == nil {
		t.Fatal("oversized original accepted")
	}
	r := CreditCloseReceipt{Version: 1, CommandID: id, BackendID: id, NodeID: id, BootID: id, GrantID: id, CommandSHA256: string(bytes.Repeat([]byte{'0'}, 64)), State: "unsettled"}
	raw, _ = json.Marshal(r)
	if _, err := DecodeCreditCloseReceipt(raw); err != nil {
		t.Fatal(err)
	}
	r.Final = json.RawMessage(`{}`)
	if r.Validate() == nil {
		t.Fatal("unsettled result carried final evidence")
	}
	r.State = "final"
	raw, _ = json.Marshal(r)
	if _, err := DecodeCreditCloseReceipt(raw); err != nil {
		t.Fatal(err)
	}
	r.Final = nil
	if r.Validate() == nil {
		t.Fatal("final omitted evidence")
	}
	r.State = "expired"
	if r.Validate() == nil {
		t.Fatal("expiry substituted for final evidence")
	}
}
