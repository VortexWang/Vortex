package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// DemandScope is immutable for an outstanding request. It is a request routing
// key, never authority: the Hub must still validate its installed delegation.
type DemandScope struct {
	HubID, DelegationID, NodeID, BootID       string
	SourceID, SnapshotID, RuleID              string
	SignerEpoch, RuleRevision, PathGeneration uint64
}

// PrepareDemand persists before dispatch. Concurrent callers and restarts get
// the original request, including amount and TTL, until its response is durably
// installed. Timeout, expiry and a larger new estimate cannot retire it.
func (s *Store) PrepareDemand(ctx context.Context, scope DemandScope, amount uint64, ttl time.Duration) (IssueRequest, error) {
	return s.prepareDemand(ctx, scope, amount, ttl, false)
}

// PrepareAutomaticDemand enforces durable refusal backoff. Explicit preparation
// remains available for a trusted caller supplying a newly reduced request.
func (s *Store) PrepareAutomaticDemand(ctx context.Context, scope DemandScope, amount uint64, ttl time.Duration) (IssueRequest, error) {
	return s.prepareDemand(ctx, scope, amount, ttl, true)
}

func (s *Store) prepareDemand(ctx context.Context, scope DemandScope, amount uint64, ttl time.Duration, automatic bool) (IssueRequest, error) {
	var result IssueRequest
	if s.owner != "agent" || amount == 0 || amount > math.MaxInt64 || ttl < time.Second || ttl > 300*time.Second || ttl%time.Second != 0 || scope.RuleID == "" || len(scope.RuleID) > 128 || scope.SignerEpoch == 0 || scope.RuleRevision == 0 || scope.PathGeneration == 0 {
		return result, ErrAuthorizationDenied
	}
	for _, id := range []string{scope.HubID, scope.DelegationID, scope.NodeID, scope.BootID, scope.SourceID, scope.SnapshotID} {
		if !validIssuerUUID(id) {
			return result, ErrAuthorizationDenied
		}
	}
	key, err := json.Marshal(scope)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request FROM authorization_demands WHERE scope=? AND state='pending'`, key).Scan(&raw)
	if err == nil {
		if err := json.Unmarshal(raw, &result); err != nil {
			return IssueRequest{}, err
		}
		if err := tx.Commit(); err != nil {
			return IssueRequest{}, err
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if automatic {
		if err := checkDemandRetryTx(ctx, tx, key, time.Now()); err != nil {
			return IssueRequest{}, err
		}
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_demands WHERE state='pending'`).Scan(&pending); err != nil {
		return result, err
	}
	if pending >= 4096 {
		return result, ErrAuthorizationDenied
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return result, err
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	h := hex.EncodeToString(id[:])
	result = IssueRequest{ID: h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], HubID: scope.HubID, DelegationID: scope.DelegationID, NodeID: scope.NodeID, BootID: scope.BootID, RawBytes: amount, TTL: ttl}
	raw, err = json.Marshal(result)
	if err != nil {
		return IssueRequest{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_demands(request_id,scope,request,state) VALUES(?,?,?,'pending')`, result.ID, key, raw); err != nil {
		return IssueRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return IssueRequest{}, err
	}
	return result, nil
}

// CompleteDemand accepts only a response already committed by the authenticated
// authorization adapter. It does not install authority or refund unknown issues.
// Even an expired response must first be installed/accounted, then a new request
// can be made. The old grant's remaining/unknown liabilities are untouched.
func (s *Store) CompleteDemand(ctx context.Context, requestID, grantID string) error {
	if s.owner != "agent" || !validIssuerUUID(requestID) || !validIssuerUUID(grantID) {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := completeDemandTx(ctx, tx, requestID, grantID); err != nil {
		return err
	}
	return tx.Commit()
}

func completeDemandTx(ctx context.Context, tx *sql.Tx, requestID, grantID string) error {
	var key, raw []byte
	var state, completedGrant string
	if err := tx.QueryRowContext(ctx, `SELECT scope,request,state,grant_id FROM authorization_demands WHERE request_id=?`, requestID).Scan(&key, &raw, &state, &completedGrant); err != nil {
		return err
	}
	if state == "denied" {
		return ErrAuthorizationUpdateConflict
	}
	if state == "applied" {
		if completedGrant != grantID {
			return ErrAuthorizationUpdateConflict
		}
		return nil
	}
	var scope DemandScope
	var request IssueRequest
	if err := json.Unmarshal(key, &scope); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	var payload []byte
	var boot string
	if err := tx.QueryRowContext(ctx, `SELECT payload,boot_id FROM authorization_updates WHERE node_id=? AND update_id=?`, scope.NodeID, requestID).Scan(&payload, &boot); err != nil {
		return err
	}
	var response struct {
		Authorizations []struct {
			ID string `json:"authorization_id"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return err
	}
	found := false
	for _, record := range response.Authorizations {
		found = found || record.ID == grantID
	}
	if !found || boot != scope.BootID {
		return ErrAuthorizationDenied
	}
	r, _, err := readAuthorizationTx(ctx, tx, scope.NodeID, grantID)
	if err != nil {
		return err
	}
	if r.IssuerID != scope.HubID || r.Kind != "credit_grant" || r.ParentAuthorizationID != scope.DelegationID || r.SourceID != scope.SourceID || r.BootID != scope.BootID || r.RuleID != scope.RuleID || r.RuleRevision != scope.RuleRevision || r.PathGeneration != scope.PathGeneration || r.SignerEpoch != scope.SignerEpoch || r.BillingSnapshotID != scope.SnapshotID || r.AmountBytes != request.RawBytes {
		return ErrAuthorizationDenied
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_demands SET state='applied',grant_id=? WHERE request_id=? AND state='pending'`, grantID, requestID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM demand_retry WHERE scope=?`, key)
	return err
}

// Called inside authorization installation: malformed or mismatched responses
// roll back both the new authority and request completion.
func resolveDemandUpdateTx(ctx context.Context, tx *sql.Tx, updateID string, records []AuthorizationRecord) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_demands WHERE request_id=?`, updateID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var grant string
	for _, r := range records {
		if r.Kind == "credit_grant" {
			if grant != "" {
				return ErrAuthorizationDenied
			}
			grant = r.AuthorizationID
		}
	}
	if grant == "" {
		return ErrAuthorizationDenied
	}
	return completeDemandTx(ctx, tx, updateID, grant)
}
