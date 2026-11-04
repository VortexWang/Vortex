package accounting

import (
	"context"
	"errors"
	"strings"
)

const maxGrantRecords = 4096

var ErrGrantConflict = errors.New("credit: grant identity or transition conflict")

type GrantOutcome string

const (
	GrantPending  GrantOutcome = "pending"
	GrantAccepted GrantOutcome = "accepted"
	GrantRejected GrantOutcome = "rejected"
	GrantUnknown  GrantOutcome = "unknown"
)

// GrantReply is a trusted, process-local injection, not a network authorization
// endpoint. NotIssuedProof means the injected authority proved non-issuance;
// elapsed time alone is never such proof.
type GrantReply struct {
	RequestID      string
	SnapshotID     string
	GrantID        string
	Amount         int64
	Outcome        GrantOutcome
	NotIssuedProof bool
}
type grantRecord struct {
	amount  int64
	outcome GrantOutcome
	grantID string
}

func (g *CreditGate) BeginGrant(ctx context.Context, id string, amount int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.HasPrefix(id, "local:") {
		return ErrGrantConflict
	}
	return g.beginGrant(id, amount)
}
func (g *CreditGate) beginGrant(id string, amount int64) error {
	if id == "" || len(id) > 256 || amount < 1 || amount > g.cfg.GrantTarget {
		return ErrGrantConflict
	}
	if r, ok := g.requests[id]; ok {
		if r.amount == amount {
			return nil
		}
		return ErrGrantConflict
	}
	if len(g.requests) >= maxGrantRecords || amount > g.state.Unissued {
		return ErrGrantConflict
	}
	g.requests[id] = grantRecord{amount: amount, outcome: GrantPending}
	g.state.Unissued -= amount
	g.state.PendingGrant += amount
	g.state.GrantRecords++
	return nil
}
func (g *CreditGate) ApplyGrant(ctx context.Context, r GrantReply) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.HasPrefix(r.RequestID, "local:") || strings.HasPrefix(r.GrantID, "local:") {
		return ErrGrantConflict
	}
	return g.applyGrant(r)
}
func (g *CreditGate) applyGrant(r GrantReply) error {
	old, ok := g.requests[r.RequestID]
	if !ok || r.SnapshotID != g.cfg.SnapshotID || r.Amount != old.amount {
		return ErrGrantConflict
	}
	switch r.Outcome {
	case GrantAccepted:
		if r.GrantID == "" || len(r.GrantID) > 256 {
			return ErrGrantConflict
		}
		if owner, ok := g.grantOwners[r.GrantID]; ok && owner != r.RequestID {
			return ErrGrantConflict
		}
	case GrantUnknown, GrantRejected:
		if r.GrantID != "" {
			return ErrGrantConflict
		}
	default:
		return ErrGrantConflict
	}
	if old.outcome == r.Outcome && old.grantID == r.GrantID {
		return nil
	}
	if old.outcome == GrantAccepted || old.outcome == GrantRejected {
		return ErrGrantConflict
	}
	if old.outcome == GrantUnknown && r.Outcome == GrantRejected && !r.NotIssuedProof {
		return ErrGrantConflict
	}
	if old.outcome == GrantPending {
		g.state.PendingGrant -= old.amount
	} else {
		g.state.UnknownGrant -= old.amount
	}
	switch r.Outcome {
	case GrantAccepted:
		g.state.Available += old.amount
		g.state.Grants++
		g.grantOwners[r.GrantID] = r.RequestID
	case GrantUnknown:
		g.state.UnknownGrant += old.amount
	case GrantRejected:
		g.state.Unissued += old.amount
	}
	old.outcome = r.Outcome
	old.grantID = r.GrantID
	g.requests[r.RequestID] = old
	return nil
}
