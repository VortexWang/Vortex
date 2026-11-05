package apply

import "testing"

func TestTransactionIdentityAndUnknownAbort(t *testing.T) {
	m := NewManager()
	id := [16]byte{1}
	if _, err := m.Prepare(id, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Prepare(id, 2); err != ErrConflict {
		t.Fatalf("generation reuse: %v", err)
	}
	if _, err := m.Commit(id, true); err != ErrUnknown {
		t.Fatalf("commit: %v", err)
	}
	if _, err := m.Abort(id); err != ErrConflict {
		t.Fatalf("unknown abort: %v", err)
	}
	if _, err := m.Release(id, false); err != ErrConflict {
		t.Fatalf("unknown release: %v", err)
	}
}

func TestActiveCommitIsIdempotent(t *testing.T) {
	m := NewManager()
	id := [16]byte{2}
	if _, err := m.Prepare(id, 1); err != nil {
		t.Fatal(err)
	}
	first, err := m.Commit(id, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Commit(id, false)
	if err != nil || first != second {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
}

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
