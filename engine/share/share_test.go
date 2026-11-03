package share

import "testing"

func TestFencingAndPreparedCannotCommitStale(t *testing.T) {
	m := NewManager()
	var k Key
	a, _ := m.Prepare(k, 50, 5)
	b, _ := m.Prepare(k, 60, 6)
	if _, err := m.Commit(k, a.Generation); err == nil {
		t.Fatal("stale revision committed")
	}
	if r, err := m.Commit(k, b.Generation); err != nil || !r.Active {
		t.Fatalf("commit=%+v err=%v", r, err)
	}
}
