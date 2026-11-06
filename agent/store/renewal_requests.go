package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

var ErrRenewalConflict = errors.New("store: renewal identity or result conflicts with durable intent")

// RenewalIntent is a transport journal, never proof of authority. Received
// success remains outstanding until the fully validated authorization commits.
type RenewalIntent struct {
	IssuerID string
	Request  *controlv1.RenewalRequest
	State    string
	Result   *controlv1.RenewalResult
}

func renewalIntentTx(ctx context.Context, tx *sql.Tx, requestID string) (RenewalIntent, error) {
	var intent RenewalIntent
	var payload, digest, result, resultDigest []byte
	if err := tx.QueryRowContext(ctx, `SELECT issuer_id,payload,payload_sha256,state,result,result_sha256 FROM renewal_requests WHERE request_id=?`, requestID).Scan(&intent.IssuerID, &payload, &digest, &intent.State, &result, &resultDigest); err != nil {
		return intent, err
	}
	request, err := contracts.DecodeRenewalRequest(payload)
	if err != nil || request.RequestId != requestID || !bytes.Equal(request.RequestSha256, digest) {
		return RenewalIntent{}, ErrRenewalConflict
	}
	intent.Request = request
	if len(result) != 0 {
		intent.Result = new(controlv1.RenewalResult)
		if err := proto.Unmarshal(result, intent.Result); err != nil {
			return RenewalIntent{}, ErrRenewalConflict
		}
		canonical, err := contracts.EncodeRenewalResult(request, intent.Result)
		sum := sha256.Sum256(result)
		if err != nil || !bytes.Equal(canonical, result) || !bytes.Equal(sum[:], resultDigest) {
			return RenewalIntent{}, ErrRenewalConflict
		}
	}
	if intent.State != "pending" && intent.Result == nil || intent.State == "received" && intent.Result.Code != "ok" || intent.State == "installed" && intent.Result.Code != "ok" || intent.State == "rejected" && (intent.Result.Code == "ok" || intent.Result.Retryable) {
		return RenewalIntent{}, ErrRenewalConflict
	}
	return intent, nil
}

// PersistRenewalIntent must finish before sending. The selected authenticated
// issuer is immutable. A failover cannot create another request for an unresolved
// lineage, even with a new boot, signer epoch or path generation.
func (s *Store) PersistRenewalIntent(ctx context.Context, issuerID string, request *controlv1.RenewalRequest) error {
	return s.persistRenewalIntent(ctx, issuerID, request, "")
}

// PersistTriggeredRenewal binds one automatic attempt to an immutable grant.
// A terminal refusal cannot create an unbounded stream of fresh request IDs
// every scheduler tick; a new grant is required to establish a new trigger.
func (s *Store) PersistTriggeredRenewal(ctx context.Context, issuerID string, request *controlv1.RenewalRequest, grantID string) error {
	if !validIssuerUUID(grantID) {
		return ErrAuthorizationDenied
	}
	return s.persistRenewalIntent(ctx, issuerID, request, grantID)
}

func (s *Store) persistRenewalIntent(ctx context.Context, issuerID string, request *controlv1.RenewalRequest, basis string) error {
	if s.owner != "agent" || !validIssuerUUID(issuerID) {
		return ErrAuthorizationDenied
	}
	payload, err := contracts.EncodeRenewalRequest(request)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	previous, err := renewalIntentTx(ctx, tx, request.RequestId)
	if err == nil {
		if previous.IssuerID != issuerID || !proto.Equal(previous.Request, request) {
			return ErrRenewalConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if basis != "" {
		grant, _, err := readAuthorizationTx(ctx, tx, request.NodeId, basis)
		if err != nil {
			return err
		}
		issued, e1 := time.Parse(time.RFC3339Nano, grant.IssuedAt)
		expires, e2 := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
		if e1 != nil || e2 != nil || grant.Kind != "credit_grant" || grant.ShareReferenceID == "" || grant.IssuerID != issuerID || grant.DelegationID != request.DelegationId || grant.BootID != request.BootId || grant.SourceID != request.SourceId || grant.RuleID != request.RuleId || grant.RuleRevision != request.RuleRevision || grant.PathGeneration != request.PathGeneration || grant.SignerEpoch != request.SignerEpoch || grant.BillingSnapshotID != request.BillingSnapshotId || request.RequestedRawBytes > grant.AmountBytes || time.Duration(request.RequestedTtlSeconds)*time.Second > expires.Sub(issued) {
			return ErrRenewalConflict
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT request_id FROM renewal_requests WHERE node_id=? AND trigger_authorization_id=?`, request.NodeId, basis).Scan(&previous)
		if err == nil {
			return ErrRenewalConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var outstanding string
	err = tx.QueryRowContext(ctx, `SELECT request_id FROM renewal_requests WHERE node_id=? AND delegation_id=? AND source_id=? AND rule_id=? AND snapshot_id=? AND state IN ('pending','received')`, request.NodeId, request.DelegationId, request.SourceId, request.RuleId, request.BillingSnapshotId).Scan(&outstanding)
	if err == nil {
		return ErrRenewalConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano) // observational, never an authority deadline
	_, err = tx.ExecContext(ctx, `INSERT INTO renewal_requests(request_id,issuer_id,node_id,boot_id,delegation_id,source_id,rule_id,snapshot_id,payload,payload_sha256,trigger_authorization_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, request.RequestId, issuerID, request.NodeId, request.BootId, request.DelegationId, request.SourceId, request.RuleId, request.BillingSnapshotId, payload, request.RequestSha256, basis, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RenewalIntent(ctx context.Context, requestID string) (RenewalIntent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RenewalIntent{}, err
	}
	defer tx.Rollback()
	intent, err := renewalIntentTx(ctx, tx, requestID)
	if err != nil {
		return RenewalIntent{}, err
	}
	return intent, tx.Commit()
}

// PendingRenewalIntents is a bounded replay page for the SAME boot and issuer.
// Old-boot requests remain readable by ID for settlement, but are never returned
// for a new boot to send as new authority. Pages are ordered by immutable ID.
func (s *Store) PendingRenewalIntents(ctx context.Context, issuerID, nodeID, bootID, afterID string) ([]RenewalIntent, error) {
	if s.owner != "agent" || !validIssuerUUID(issuerID) || !validIssuerUUID(nodeID) || !validIssuerUUID(bootID) {
		return nil, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT request_id FROM renewal_requests WHERE issuer_id=? AND node_id=? AND boot_id=? AND request_id>? AND state IN ('pending','received') ORDER BY request_id LIMIT 128`, issuerID, nodeID, bootID, afterID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var intents []RenewalIntent
	for _, id := range ids {
		intent, err := renewalIntentTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if intent.IssuerID != issuerID || intent.Request.NodeId != nodeID || intent.Request.BootId != bootID {
			return nil, ErrRenewalConflict
		}
		intents = append(intents, intent)
	}
	return intents, tx.Commit()
}

// PersistRenewalResult is called only with the current authenticated issuer.
// Retrying a transient rejection retains the same intent. Receiving a success
// does not install rights or release any usage hold or unknown liability.
func (s *Store) PersistRenewalResult(ctx context.Context, issuerID string, result *controlv1.RenewalResult) error {
	if s.owner != "agent" || result == nil {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	intent, err := renewalIntentTx(ctx, tx, result.RequestId)
	if err != nil {
		return err
	}
	if intent.IssuerID != issuerID {
		return ErrRenewalConflict
	}
	payload, err := contracts.EncodeRenewalResult(intent.Request, result)
	if err != nil {
		return err
	}
	if intent.State != "pending" {
		if !proto.Equal(intent.Result, result) {
			return ErrRenewalConflict
		}
		return tx.Commit()
	}
	state := "rejected"
	if result.Retryable {
		state = "pending"
	}
	if result.Code == "ok" {
		state = "received"
	}
	sum := sha256.Sum256(payload)
	_, err = tx.ExecContext(ctx, `UPDATE renewal_requests SET state=?,result=?,result_sha256=?,updated_at=? WHERE request_id=?`, state, payload, sum[:], time.Now().UTC().Format(time.RFC3339Nano), result.RequestId)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkRenewalInstalled may run after a crash between installation and journal
// completion. It requires the exact correlated update already persisted by the
// authorization validator; an old boot or an opaque received payload is not enough.
func (s *Store) MarkRenewalInstalled(ctx context.Context, issuerID, nodeID, bootID, requestID, updateID string) error {
	if s.owner != "agent" {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	intent, err := renewalIntentTx(ctx, tx, requestID)
	if err != nil {
		return err
	}
	if intent.IssuerID != issuerID || intent.Request.NodeId != nodeID || intent.Request.BootId != bootID || (intent.State != "received" && intent.State != "installed") {
		return ErrRenewalConflict
	}
	var payload, digest []byte
	var storedIssuer, storedBoot, storedPath, storedEpoch string
	if err := tx.QueryRowContext(ctx, `SELECT issuer_id,boot_id,payload,payload_sha256,path_generation,signer_epoch FROM authorization_updates WHERE node_id=? AND update_id=?`, nodeID, updateID).Scan(&storedIssuer, &storedBoot, &payload, &digest, &storedPath, &storedEpoch); err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	if storedIssuer != issuerID || storedBoot != bootID || !bytes.Equal(digest, sum[:]) || !bytes.Equal(payload, intent.Result.AuthorizationUpdate.Payload) {
		return ErrRenewalConflict
	}
	if storedPath != fmt.Sprint(intent.Request.PathGeneration) || storedEpoch != fmt.Sprint(intent.Request.SignerEpoch) {
		return ErrRenewalConflict
	}
	var correlation struct {
		SchemaVersion string `json:"schema_version"`
		RequestID     string `json:"renewal_request_id"`
		Digest        string `json:"renewal_request_sha256"`
	}
	if err := json.Unmarshal(payload, &correlation); err != nil || correlation.SchemaVersion != "1.2.0" || correlation.RequestID != requestID || correlation.Digest != hex.EncodeToString(intent.Request.RequestSha256) {
		return fmt.Errorf("%w: missing approved authorization correlation", ErrRenewalConflict)
	}
	_, err = tx.ExecContext(ctx, `UPDATE renewal_requests SET state='installed',updated_at=? WHERE request_id=?`, time.Now().UTC().Format(time.RFC3339Nano), requestID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
