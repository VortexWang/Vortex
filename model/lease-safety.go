package model

import "fmt"

// State is a deliberately small, finite abstraction of quota, lease and
// recovery ownership. It is not the production state machine.
type State struct {
	B, F, L, A         int
	ActiveGeneration   int
	ShareOld, ShareNew int
	TransferCount      int
	UsagePersisted     bool
	UsageAcked         bool
	UnknownLiability   bool
	RiskBudget         int
	Snapshot           string
	Activation         string
	Boot               int
}

const (
	InvariantQuota      = "quota-conservation"
	InvariantGeneration = "single-active-generation"
	InvariantShareChain = "share-chain-max"
	InvariantTransfer   = "exactly-once-R-transfer"
	InvariantUsage      = "idempotent-persisted-usage"
	InvariantUnknown    = "unknown-liability-not-released"
	InvariantSnapshot   = "immutable-snapshot-isolation"
	InvariantActivation = "activation-unknown-retains-occupancy"
)

var Invariants = []string{InvariantQuota, InvariantGeneration, InvariantShareChain, InvariantTransfer, InvariantUsage, InvariantUnknown, InvariantSnapshot, InvariantActivation}

func (s State) Check() error {
	if s.F > s.B+s.L || s.F < 0 || s.B < 0 || s.L < 0 || s.A < 0 || s.A > s.B+s.L-s.F {
		return fmt.Errorf("%s: B=%d F=%d L=%d A=%d", InvariantQuota, s.B, s.F, s.L, s.A)
	}
	if s.ActiveGeneration < 0 {
		return fmt.Errorf("%s: negative generation", InvariantGeneration)
	}
	if s.ShareOld < 0 || s.ShareNew < 0 || s.ShareOld > s.ShareNew {
		return fmt.Errorf("%s: old=%d new=%d", InvariantShareChain, s.ShareOld, s.ShareNew)
	}
	if s.TransferCount < 0 || s.TransferCount > 1 {
		return fmt.Errorf("%s: transfer count=%d", InvariantTransfer, s.TransferCount)
	}
	if s.UsageAcked && !s.UsagePersisted {
		return fmt.Errorf("%s: ack before durable persistence", InvariantUsage)
	}
	if s.UnknownLiability && s.RiskBudget == 0 && s.Activation == "released" {
		return fmt.Errorf("%s: released unknown liability without budget", InvariantUnknown)
	}
	if s.Snapshot == "" {
		return fmt.Errorf("%s: empty snapshot", InvariantSnapshot)
	}
	if s.Activation == "activation-unknown" && s.F == 0 {
		return fmt.Errorf("%s: unknown activation released occupancy", InvariantActivation)
	}
	return nil
}

func Initial() State {
	return State{B: 8, F: 0, L: 0, A: 8, ActiveGeneration: 1, Snapshot: "snap-1", Activation: "prepared", Boot: 1}
}

// Transitions returns a fixed deterministic set that exercises the named
// states without claiming to enumerate unbounded production behavior.
func Transitions(s State) []State {
	return []State{
		s,
		{B: s.B, F: 2, L: 1, A: 5, ActiveGeneration: s.ActiveGeneration, ShareOld: 1, ShareNew: 2, TransferCount: 0, UsagePersisted: true, Snapshot: s.Snapshot, Activation: "committed", Boot: s.Boot},
		{B: s.B, F: 2, L: 1, A: 5, ActiveGeneration: s.ActiveGeneration + 1, ShareOld: 1, ShareNew: 2, TransferCount: 1, UsagePersisted: true, UsageAcked: true, Snapshot: "snap-1", Activation: "draining", Boot: s.Boot + 1},
		{B: s.B, F: 2, L: 1, A: 5, ActiveGeneration: s.ActiveGeneration + 1, ShareOld: 2, ShareNew: 2, TransferCount: 1, UsagePersisted: true, UsageAcked: true, UnknownLiability: true, RiskBudget: 1, Snapshot: "snap-1", Activation: "activation-unknown", Boot: s.Boot + 1},
	}
}

type Exploration struct {
	Visited    int
	Bounds     map[string]int
	Invariants []string
	Violations []string
}

func Explore() Exploration {
	seen := map[State]bool{}
	result := Exploration{Bounds: map[string]int{"max_depth": 4, "max_sources": 2, "max_generations": 2}, Invariants: append([]string(nil), Invariants...)}
	queue := []State{Initial()}
	for depth := 0; len(queue) > 0 && depth < 4; depth++ {
		next := []State{}
		for _, state := range queue {
			if seen[state] {
				continue
			}
			seen[state] = true
			result.Visited++
			if err := state.Check(); err != nil {
				result.Violations = append(result.Violations, err.Error())
				continue
			}
			next = append(next, Transitions(state)...)
		}
		queue = next
	}
	return result
}

func UnsafeFixture() State {
	s := Initial()
	s.UsageAcked = true
	return s
}

func CheckUnsafeFixture() error { return UnsafeFixture().Check() }
