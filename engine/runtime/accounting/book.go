package accounting

import (
	"context"
	"errors"
	"sync"

	core "vortex/engine/accounting"
)

// Binding describes immutable billing metadata, not a mutable routing command.
type Binding struct {
	RuleID           string `json:"rule_id"`
	ExecutionNode    string `json:"execution_node"`
	PathGeneration   uint64 `json:"path_generation"`
	PolicyRevision   uint64 `json:"policy_revision"`
	CoefficientMilli uint64 `json:"coefficient_milli"`
}
type CreditVersion struct {
	Binding Binding      `json:"binding"`
	Credit  CreditConfig `json:"credit"`
}
type BookConfig struct {
	FundingBytes      int64                    `json:"funding_bytes"`
	Active            string                   `json:"active"`
	SelectionRevision uint64                   `json:"selection_revision"`
	Snapshots         map[string]CreditVersion `json:"snapshots"`
}

// BilledUpper converts a raw upper bound with a single ceiling. It is not the
// cumulative fixed-point tenant debit. Validated raw/coefficient bounds avoid
// overflow by dividing before multiplying.
func BilledUpper(raw int64, coefficient uint64) int64 {
	return raw/1000*int64(coefficient) + (raw%1000*int64(coefficient)+999)/1000
}
func (c BookConfig) Validate() error {
	if len(c.Snapshots) < 1 || len(c.Snapshots) > 16 || c.SelectionRevision == 0 || c.FundingBytes < 0 || c.FundingBytes > 1<<60 {
		return errors.New("credit book: invalid bounded book")
	}
	if _, ok := c.Snapshots[c.Active]; !ok {
		return errors.New("credit book: active snapshot not pinned")
	}
	var reserved int64
	for id, v := range c.Snapshots {
		if id != v.Credit.SnapshotID || v.Credit.Validate() != nil || v.Binding.RuleID == "" || v.Binding.ExecutionNode == "" || v.Binding.PathGeneration == 0 || v.Binding.PolicyRevision == 0 || v.Binding.CoefficientMilli == 0 || v.Binding.CoefficientMilli > 1000000 {
			return errors.New("credit book: invalid immutable binding")
		}
		n := BilledUpper(v.Credit.Budget, v.Binding.CoefficientMilli)
		if n > c.FundingBytes-reserved {
			return errors.New("credit book: insufficient preallocated funding")
		}
		reserved += n
	}
	return nil
}

type VersionState struct {
	Binding     Binding     `json:"binding"`
	Credit      CreditState `json:"credit"`
	FundedUpper int64       `json:"funded_upper_bytes"`
	UsedUpper   int64       `json:"used_upper_bytes"`
}
type BookState struct {
	Active            string                  `json:"active"`
	SelectionRevision uint64                  `json:"selection_revision"`
	FundingBytes      int64                   `json:"funding_bytes"`
	ReservedFunding   int64                   `json:"reserved_funding_bytes"`
	Snapshots         map[string]VersionState `json:"snapshots"`
}
type CreditBook struct {
	mu     sync.Mutex
	cfg    BookConfig
	gates  map[string]*CreditGate
	owners map[string]grantOwner
}
type grantOwner struct{ snapshot, request string }

func NewCreditBook(c BookConfig) (*CreditBook, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	// Copy caller-owned maps. A config reload must never mutate a live binding.
	versions := make(map[string]CreditVersion, len(c.Snapshots))
	gates := make(map[string]*CreditGate, len(c.Snapshots))
	for id, v := range c.Snapshots {
		versions[id] = v
		gates[id], _ = NewCreditGate(v.Credit)
	}
	c.Snapshots = versions
	return &CreditBook{cfg: c, gates: gates, owners: make(map[string]grantOwner)}, nil
}
func (b *CreditBook) Select(id string, revision uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.gates[id]; !ok {
		return core.ErrSnapshotMismatch
	}
	if id == b.cfg.Active && revision == b.cfg.SelectionRevision {
		return nil
	}
	if revision <= b.cfg.SelectionRevision {
		return errors.New("credit book: stale selection revision")
	}
	b.cfg.Active = id
	b.cfg.SelectionRevision = revision
	return nil
}
func (b *CreditBook) Open(ctx context.Context, f core.Flow) (core.Session, error) {
	session, _, _, err := b.openCurrent(ctx, f)
	return session, err
}

func (b *CreditBook) openCurrent(ctx context.Context, f core.Flow) (core.Session, string, Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.cfg.Active
	session, err := b.gates[id].Open(ctx, f)
	return session, id, b.cfg.Snapshots[id].Binding, err
}

// openPolicy keeps a live flow on its original path and execution point while
// changing only the policy snapshot. Ambiguous or missing authority denies.
func (b *CreditBook) openPolicy(ctx context.Context, f core.Flow, original Binding, policy uint64) (core.Session, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, err := b.policySnapshotLocked(original, policy)
	if err != nil {
		return nil, "", err
	}
	session, err := b.gates[id].Open(ctx, f)
	return session, id, err
}
func (b *CreditBook) policySnapshotLocked(original Binding, policy uint64) (string, error) {
	id := ""
	for candidate, version := range b.cfg.Snapshots {
		binding := version.Binding
		if binding.RuleID == original.RuleID && binding.ExecutionNode == original.ExecutionNode && binding.PathGeneration == original.PathGeneration && binding.PolicyRevision == policy {
			if id != "" {
				return "", core.ErrSnapshotMismatch
			}
			id = candidate
		}
	}
	if id == "" {
		return "", core.ErrSnapshotMismatch
	}
	return id, nil
}

func (b *CreditBook) selectPolicy(policy, revision uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	binding := b.cfg.Snapshots[b.cfg.Active].Binding
	if binding.PolicyRevision == policy {
		return nil
	}
	id, err := b.policySnapshotLocked(binding, policy)
	if err != nil {
		return err
	}
	if revision <= b.cfg.SelectionRevision {
		return core.ErrSnapshotMismatch
	}
	b.cfg.Active, b.cfg.SelectionRevision = id, revision
	return nil
}

func (b *CreditBook) State() BookState {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := BookState{Active: b.cfg.Active, SelectionRevision: b.cfg.SelectionRevision, FundingBytes: b.cfg.FundingBytes, Snapshots: make(map[string]VersionState)}
	for id, v := range b.cfg.Snapshots {
		state := b.gates[id].State()
		upper := BilledUpper(v.Credit.Budget, v.Binding.CoefficientMilli)
		s.Snapshots[id] = VersionState{Binding: v.Binding, Credit: state, FundedUpper: upper, UsedUpper: BilledUpper(state.Used, v.Binding.CoefficientMilli)}
		s.ReservedFunding += upper
	}
	return s
}
func (b *CreditBook) BeginGrant(ctx context.Context, id, request string, amount int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	g, ok := b.gates[id]
	if !ok {
		return core.ErrSnapshotMismatch
	}
	return g.BeginGrant(ctx, request, amount)
}
func (b *CreditBook) ApplyGrant(ctx context.Context, r GrantReply, coefficient uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.cfg.Snapshots[r.SnapshotID]
	if !ok || coefficient != v.Binding.CoefficientMilli {
		return core.ErrSnapshotMismatch
	}
	want := grantOwner{snapshot: r.SnapshotID, request: r.RequestID}
	if owner, ok := b.owners[r.GrantID]; r.GrantID != "" && ok && owner != want {
		return ErrGrantConflict
	}
	if err := b.gates[r.SnapshotID].ApplyGrant(ctx, r); err != nil {
		return err
	}
	if r.Outcome == GrantAccepted {
		b.owners[r.GrantID] = want
	}
	return nil
}
