package accounting

import "testing"

func TestSnapshotRejectsCoefficientOrIDReuse(t *testing.T) {
	s := Snapshot{ID: "old", Coefficient: 1}
	if err := s.ValidateGrant(Grant{SnapshotID: "old", GrantID: "g", Coefficient: 3}); err != ErrSnapshotMismatch {
		t.Fatal("coefficient reuse accepted")
	}
	if err := s.ValidateGrant(Grant{SnapshotID: "new", GrantID: "g", Coefficient: 1}); err != ErrSnapshotMismatch {
		t.Fatal("snapshot reuse accepted")
	}
	if err := s.ValidateGrant(Grant{SnapshotID: "old", GrantID: "g", Coefficient: 1}); err != nil {
		t.Fatal(err)
	}
}
