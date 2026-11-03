package accounting

import "errors"

var ErrSnapshotMismatch = errors.New("accounting: immutable snapshot mismatch")

type Snapshot struct {
	RuleID      [16]byte
	Execution   [16]byte
	ID          string
	Coefficient uint64
}

type Grant struct {
	SnapshotID  string
	GrantID     string
	Coefficient uint64
}

func (s Snapshot) ValidateGrant(g Grant) error {
	if s.ID == "" || g.GrantID == "" || g.SnapshotID != s.ID || g.Coefficient != s.Coefficient {
		return ErrSnapshotMismatch
	}
	return nil
}
