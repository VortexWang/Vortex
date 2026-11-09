package contracts

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestDelegationSealConservationCanonicalAndCompleteInventoryDigest(t *testing.T) {
	id := uuid.NewString()
	digest, total, e := PartitionDigest([]IssuedPartition{{ID: id, Digest: SharePayloadDigest([]byte("issued bytes")), Raw: 51}})
	if e != nil || total != 51 {
		t.Fatal(e)
	}
	seal := DelegationSeal{Version: 1, ID: uuid.NewString(), DelegationID: uuid.NewString(), HubID: uuid.NewString(), HubBootID: uuid.NewString(), NodeID: uuid.NewString(), BootID: uuid.NewString(), SignerEpoch: 7, Raw: 103, Issued: 51, Unissued: 52, IssueCount: 1, IssueDigest: digest}
	raw, _ := json.Marshal(seal)
	if _, e = DecodeDelegationSeal(raw); e != nil {
		t.Fatal(e)
	}
	seal.Unissued++
	bad, _ := json.Marshal(seal)
	if _, e = DecodeDelegationSeal(bad); e == nil {
		t.Fatal("broken conservation accepted")
	}
	if _, e = DecodeDelegationSeal(append(raw, ' ')); e == nil {
		t.Fatal("noncanonical seal")
	}
	for _, inventory := range [][]IssuedPartition{{{ID: id, Digest: digest, Raw: 1}, {ID: id, Digest: digest, Raw: 1}}, {{ID: id, Digest: "bad", Raw: 51}}, {{ID: id, Digest: digest, Raw: 0}}} {
		if _, _, e = PartitionDigest(inventory); e == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
	changed, _, e := PartitionDigest([]IssuedPartition{{ID: id, Digest: SharePayloadDigest([]byte("different child")), Raw: 51}})
	if e != nil || changed == digest {
		t.Fatal("same-total child replacement invisible")
	}
}
