package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestKnownUsageInventoryBindsCompleteOriginalBatchSet(t *testing.T) {
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	a := KnownUsageBatch{BatchID: "a\n", SHA256: hash("first wire payload")}
	b := KnownUsageBatch{BatchID: "b", SHA256: hash("second wire payload")}
	items := []KnownUsageBatch{b, a}
	before := append([]KnownUsageBatch(nil), items...)
	want, err := BuildKnownUsageInventory(items)
	if err != nil || want.Validate() != nil || want.Count != 2 || !reflect.DeepEqual(items, before) {
		t.Fatal(want, err, items)
	}
	got, err := BuildKnownUsageInventory([]KnownUsageBatch{a, b})
	if err != nil || got != want {
		t.Fatal("order changed inventory", got, err)
	}
	for _, changed := range [][]KnownUsageBatch{{a}, {b}, {a, {BatchID: b.BatchID, SHA256: hash("changed")}}, {{BatchID: "a", SHA256: a.SHA256}, b}} {
		got, err := BuildKnownUsageInventory(changed)
		if err != nil || got == want {
			t.Fatal("changed inventory undetected", got, err)
		}
	}
	for _, bad := range [][]KnownUsageBatch{{a, a}, {{BatchID: "", SHA256: a.SHA256}}, {{BatchID: "x", SHA256: strings.ToUpper(a.SHA256)}}, {{BatchID: "x", SHA256: "00"}}} {
		if _, err := BuildKnownUsageInventory(bad); err == nil {
			t.Fatal("invalid inventory accepted", bad)
		}
	}
	empty, err := BuildKnownUsageInventory(nil)
	if err != nil || empty.Count != 0 || empty.Validate() != nil || empty == want {
		t.Fatal(empty, err)
	}
	if (KnownUsageInventory{}).Validate() == nil {
		t.Fatal("historical missing proof accepted as empty inventory")
	}
}
