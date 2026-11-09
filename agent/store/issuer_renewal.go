package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// Distinct from a scope denial so callers never guess why a renewal failed.
// It remains an authorization denial for legacy issuer callers.
var ErrIssuerQuotaExhausted = errors.Join(ErrAuthorizationDenied, errors.New("store: delegated quota exhausted"))

// RenewHubAuthorization returns only a committed terminal outcome. Rejections
// are immutable too: a time, scope or quota refusal cannot later become success
// for the same request ID after provisioning changes or a restart. Storage or
// unexpected failures return an error, never an uncommitted success/rejection.
// The caller must first bind request node/boot to its authenticated session.
func (s *Store) RenewHubAuthorization(ctx context.Context, issuer string, request *controlv1.RenewalRequest, now IssuerTime) (*controlv1.RenewalResult, error) {
	if s.owner != "hub" || !validIssuerUUID(issuer) {
		return nil, ErrAuthorizationDenied
	}
	raw, err := contracts.EncodeRenewalRequest(request)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var oldIssuer string
	var oldRequest, oldDigest, oldResult, oldResultDigest []byte
	err = tx.QueryRowContext(ctx, `SELECT issuer_id,request,request_sha256,result,result_sha256 FROM issuer_renewals WHERE request_id=?`, request.RequestId).Scan(&oldIssuer, &oldRequest, &oldDigest, &oldResult, &oldResultDigest)
	if err == nil {
		if issuer != oldIssuer || !bytes.Equal(raw, oldRequest) || !bytes.Equal(request.RequestSha256, oldDigest) {
			return nil, ErrRenewalConflict
		}
		result := new(controlv1.RenewalResult)
		if err := proto.Unmarshal(oldResult, result); err != nil {
			return nil, ErrRenewalConflict
		}
		canonical, err := contracts.EncodeRenewalResult(request, result)
		sum := sha256.Sum256(oldResult)
		if err != nil || !bytes.Equal(canonical, oldResult) || !bytes.Equal(sum[:], oldResultDigest) {
			return nil, ErrRenewalConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	r := IssueRequest{ID: request.RequestId, HubID: issuer, DelegationID: request.DelegationId, NodeID: request.NodeId, BootID: request.BootId, RawBytes: request.RequestedRawBytes, TTL: time.Duration(request.RequestedTtlSeconds) * time.Second, RenewalPayload: raw}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT renewal_attempt`); err != nil {
		return nil, err
	}
	issue := IssuedAuthorization{}
	err = checkIssuerTime(now)
	if err == nil {
		issue, err = s.issueHubAuthorizationTx(ctx, tx, r, now)
	}
	result := &controlv1.RenewalResult{RequestId: request.RequestId, RequestSha256: append([]byte(nil), request.RequestSha256...), Code: "ok"}
	if err != nil {
		// Roll back all attempted authority mutations before committing refusal.
		code := ""
		switch {
		case errors.Is(err, ErrIssuerQuotaExhausted):
			code = "quota_exhausted"
		case errors.Is(err, ErrSignerEpochClosed):
			code = "closed_epoch"
		case errors.Is(err, ErrAuthorityTimeUncertain):
			code = "time_uncertain"
		case errors.Is(err, ErrAuthorityExpired), errors.Is(err, ErrAuthorizationGenerationStale):
			code = "stale_revision"
		case errors.Is(err, ErrAuthorizationDenied), errors.Is(err, ErrNewAuthorityInAutonomy), errors.Is(err, ErrAuthorityHandoffRequired):
			code = "scope_denied"
		default:
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO renewal_attempt`); err != nil {
			return nil, err
		}
		result.Code = code
	} else {
		result.AuthorizationUpdate = &controlv1.AuthorizationUpdate{Generation: issue.Generation, Payload: issue.Payload}
	}
	if _, err := tx.ExecContext(ctx, `RELEASE renewal_attempt`); err != nil {
		return nil, err
	}
	encoded, err := contracts.EncodeRenewalResult(request, result)
	if err != nil {
		return nil, err
	}
	if result.Code == "ok" && !bytes.Equal(encoded, issue.RenewalResult) {
		return nil, ErrRenewalConflict
	}
	sum := sha256.Sum256(encoded)
	if _, err := tx.ExecContext(ctx, `INSERT INTO issuer_renewals(request_id,issuer_id,request,request_sha256,result,result_sha256) VALUES(?,?,?,?,?,?)`, request.RequestId, issuer, raw, request.RequestSha256, encoded, sum[:]); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
