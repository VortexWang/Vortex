package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

var ErrDemandDefinitivelyDenied = fmt.Errorf("%w: demand definitively denied", ErrAuthorizationDenied)
var ErrDemandInsufficientFunds = fmt.Errorf("%w: demand exceeds available funds", ErrAuthorizationDenied)

type DemandOutcome struct {
	Authorization IssuedAuthorization
	// Nonempty only for a committed definitive non-issuance outcome.
	Denial string
}

// ResolveHubDemand commits one immutable outcome. Invalid envelopes and storage
// failures produce no definitive denial. The caller authenticates node and Hub
// identities before entering this boundary. No timeout may be mapped to Denial.
func (s *Store) ResolveHubDemand(ctx context.Context, r IssueRequest, now IssuerTime) (DemandOutcome, error) {
	if s.owner != "hub" || r.RawBytes == 0 || r.RawBytes > math.MaxInt64 || r.TTL < time.Second || r.TTL > 300*time.Second || r.TTL%time.Second != 0 {
		return DemandOutcome{}, ErrAuthorizationDenied
	}
	for _, id := range []string{r.ID, r.HubID, r.DelegationID, r.NodeID, r.BootID} {
		if !validIssuerUUID(id) {
			return DemandOutcome{}, ErrAuthorizationDenied
		}
	}
	if err := checkIssuerTime(now); err != nil {
		return DemandOutcome{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return DemandOutcome{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DemandOutcome{}, err
	}
	defer tx.Rollback()
	var prior []byte
	var reason string
	err = tx.QueryRowContext(ctx, `SELECT request,reason FROM issuer_demand_denials WHERE request_id=?`, r.ID).Scan(&prior, &reason)
	if err == nil {
		if !bytes.Equal(prior, raw) {
			return DemandOutcome{}, ErrAuthorizationUpdateConflict
		}
		if err := tx.Commit(); err != nil {
			return DemandOutcome{}, err
		}
		return DemandOutcome{Denial: reason}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return DemandOutcome{}, err
	}
	issued, err := s.issueHubAuthorizationTx(ctx, tx, r, now)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return DemandOutcome{}, err
		}
		return DemandOutcome{Authorization: issued}, nil
	}
	// Conflict/time uncertainty/closed signer and storage errors retain pending
	// identity. Only these authority decisions prove this request issued nothing.
	switch {
	case errors.Is(err, ErrDemandInsufficientFunds):
		reason = "insufficient_funds"
	case errors.Is(err, ErrAuthorityExpired):
		reason = "expired"
	case errors.Is(err, ErrAuthorizationDenied), errors.Is(err, ErrNewAuthorityInAutonomy):
		reason = "invalid_scope"
	default:
		return DemandOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO issuer_demand_denials(request_id,request,reason) VALUES(?,?,?)`, r.ID, raw, reason); err != nil {
		return DemandOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return DemandOutcome{}, err
	}
	return DemandOutcome{Denial: reason}, nil
}
