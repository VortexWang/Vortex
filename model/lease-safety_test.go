package model

import "testing"

func TestExploreSafeModel(t *testing.T) {
	r := Explore()
	if r.Visited < 3 {
		t.Fatalf("visited=%d, want bounded transitions", r.Visited)
	}
	if len(r.Violations) != 0 {
		t.Fatalf("safe model violations: %v", r.Violations)
	}
}

func TestUnsafeFixture(t *testing.T) {
	if err := CheckUnsafeFixture(); err == nil {
		t.Fatal("unsafe fixture was accepted")
	}
}
