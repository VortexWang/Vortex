package apply

import "testing"

func TestIdempotentUnknownAndFencing(t *testing.T) {
	m := NewManager()
	var id [16]byte
	id[0] = 1
	if _, e := m.Prepare(id, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Commit(id, true); e != ErrUnknown {
		t.Fatalf("want unknown got %v", e)
	}
	if _, e := m.Commit(id, true); e != ErrUnknown {
		t.Fatalf("unknown not idempotent: %v", e)
	}
	var id2 [16]byte
	id2[0] = 2
	if _, e := m.Prepare(id2, 2); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Commit(id2, false); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Release(id, false); e == nil {
		t.Fatal("released without proof")
	}
}
