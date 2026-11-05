package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrAuthorizationDenied = errors.New("store: durable authorization does not permit this operation")

// AuthorizationUse pins the caller's local rule and billing identity. The time
// interval must come from a trusted conservative clock. A single wall-clock
// sample after restart does not establish trusted time.
type AuthorizationUse struct {
	SessionID                       string
	RuntimeLeaseID, ShareLeaseID    string
	NodeID, BootID, AuthorizationID string
	Kind, RuleID, BillingSnapshotID string
	RuleRevision, PathGeneration    uint64
	Earliest, Latest                time.Time
	TrustedTime                     bool
	ExistingSession                 bool
}

// CheckStoredAuthorization checks a persisted lease or grant without issuing,
// renewing, spending or releasing it. Credit amounts returned here are limits,
// not proof of remaining quota; callers still require an atomic reservation.
// The result is a snapshot, not a capability to write after a later revocation.
func (s *Store) CheckStoredAuthorization(ctx context.Context, use AuthorizationUse) (AuthorizationRecord, error) {
	var record AuthorizationRecord
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return record, err
	}
	defer tx.Rollback()
	record, err = checkAuthorizationTx(ctx, tx, use)
	if err != nil {
		return AuthorizationRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthorizationRecord{}, err
	}
	return record, nil
}

// readAuthorizationTx also reads expired records as immutable lineage evidence.
// It does not grant permission to use them.
func readAuthorizationTx(ctx context.Context, tx *sql.Tx, nodeID, authorizationID string) (AuthorizationRecord, string, error) {
	var record AuthorizationRecord
	var state, ruleRevision, pathGeneration, signerEpoch, amount, rate, connections string
	err := tx.QueryRowContext(ctx, `SELECT authorization_id,parent_authorization_id,source_id,kind,rule_id,rule_revision,path_generation,signer_epoch,boot_id,issued_at,not_before,expires_at,hard_deadline,billing_snapshot_id,amount_bytes,rate_bps,connections,state FROM authorization_records WHERE node_id=? AND authorization_id=?`, nodeID, authorizationID).Scan(
		&record.AuthorizationID, &record.ParentAuthorizationID, &record.SourceID, &record.Kind, &record.RuleID,
		&ruleRevision, &pathGeneration, &signerEpoch, &record.BootID, &record.IssuedAt, &record.NotBefore,
		&record.ExpiresAt, &record.HardDeadline, &record.BillingSnapshotID, &amount, &rate, &connections, &state)
	if err != nil {
		return AuthorizationRecord{}, "", fmt.Errorf("%w: %w", ErrAuthorizationDenied, err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT issuer_id FROM authorization_records WHERE node_id=? AND authorization_id=?`, nodeID, authorizationID).Scan(&record.IssuerID); err != nil {
		return AuthorizationRecord{}, "", err
	}
	for _, field := range []struct {
		text  string
		value *uint64
	}{
		{ruleRevision, &record.RuleRevision}, {pathGeneration, &record.PathGeneration},
		{signerEpoch, &record.SignerEpoch}, {amount, &record.AmountBytes},
		{rate, &record.RateBPS}, {connections, &record.Connections},
	} {
		*field.value, err = strconv.ParseUint(field.text, 10, 64)
		if err != nil {
			return AuthorizationRecord{}, "", ErrAuthorizationDenied
		}
	}
	return record, state, nil
}

func checkAuthorizationTx(ctx context.Context, tx *sql.Tx, use AuthorizationUse) (AuthorizationRecord, error) {
	var record AuthorizationRecord
	if !use.TrustedTime || use.Earliest.IsZero() || use.Latest.Before(use.Earliest) {
		return record, ErrAuthorityTimeUncertain
	}
	if use.NodeID == "" || use.BootID == "" || use.AuthorizationID == "" || use.Kind == "" || use.RuleID == "" || use.RuleRevision == 0 || use.PathGeneration == 0 {
		return record, ErrAuthorizationDenied
	}
	record, state, err := readAuthorizationTx(ctx, tx, use.NodeID, use.AuthorizationID)
	if err != nil {
		return AuthorizationRecord{}, err
	}
	if state != "active" || record.BootID != use.BootID || record.Kind != use.Kind || record.RuleID != use.RuleID || record.RuleRevision != use.RuleRevision || record.PathGeneration != use.PathGeneration || (record.Kind == "credit_grant" || record.BillingSnapshotID != "") && record.BillingSnapshotID != use.BillingSnapshotID {
		return AuthorizationRecord{}, ErrAuthorizationDenied
	}
	if record.Kind == "credit_grant" {
		if err := checkSourceLineageTx(ctx, tx, use.NodeID, record); err != nil {
			return AuthorizationRecord{}, err
		}
	}

	notBefore, e1 := time.Parse(time.RFC3339Nano, record.NotBefore)
	expires, e2 := time.Parse(time.RFC3339Nano, record.ExpiresAt)
	deadline, e3 := time.Parse(time.RFC3339Nano, record.HardDeadline)
	if e1 != nil || e2 != nil || e3 != nil || use.Earliest.Before(notBefore) || !use.Latest.Before(expires) || !use.Latest.Before(deadline) {
		return AuthorizationRecord{}, ErrAuthorizationDenied
	}
	// Signer epoch and path generation are intentionally independent. Epoch
	// handoff does not invalidate an otherwise valid outstanding authorization.
	// Old paths can only drain sessions that already existed.
	if !use.ExistingSession {
		current, err := stateValue(ctx, tx, "authorization_path_generation")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return AuthorizationRecord{}, ErrAuthorizationDenied
			}
			return AuthorizationRecord{}, err
		}
		generation, err := strconv.ParseUint(current, 10, 64)
		if err != nil || generation != use.PathGeneration {
			return AuthorizationRecord{}, ErrAuthorizationDenied
		}
	}
	if use.RuntimeLeaseID != "" || use.ShareLeaseID != "" {
		if record.Kind != "credit_grant" || use.RuntimeLeaseID == "" || use.ShareLeaseID == "" {
			return AuthorizationRecord{}, ErrAuthorizationDenied
		}
		for kind, id := range map[string]string{"runtime_lease": use.RuntimeLeaseID, "share_lease": use.ShareLeaseID} {
			leaseUse := use
			leaseUse.RuntimeLeaseID, leaseUse.ShareLeaseID, leaseUse.SessionID = "", "", ""
			leaseUse.AuthorizationID, leaseUse.Kind = id, kind
			if _, err := checkAuthorizationTx(ctx, tx, leaseUse); err != nil {
				return AuthorizationRecord{}, err
			}
		}
	}
	if use.SessionID != "" {
		var boot, grant, runtimeLease, shareLease, state string
		if err := tx.QueryRowContext(ctx, `SELECT boot_id,grant_id,runtime_lease_id,share_lease_id,state FROM runtime_sessions WHERE node_id=? AND session_id=?`, use.NodeID, use.SessionID).Scan(&boot, &grant, &runtimeLease, &shareLease, &state); err != nil {
			return AuthorizationRecord{}, err
		}
		if state != "open" || boot != use.BootID || grant != use.AuthorizationID || runtimeLease != use.RuntimeLeaseID || shareLease != use.ShareLeaseID {
			return AuthorizationRecord{}, ErrAuthorizationDenied
		}
	}
	return record, nil
}

// ReserveAuthorizedUsage atomically checks the persisted grant and deducts its
// full byte upper bound in the same FULL SQLite transaction that records the
// Engine pre-write reservation. Duplicate retries never debit twice.
func (s *Store) ReserveAuthorizedUsage(ctx context.Context, reservation UsageReservation, use AuthorizationUse) (string, error) {
	if reservation.AuthorizationID != use.AuthorizationID || reservation.NodeID != use.NodeID || reservation.BootID != use.BootID || reservation.BatchID == "" || reservation.FlowKey == "" || reservation.Reserved == 0 || reservation.SnapshotID != use.BillingSnapshotID || reservation.RuleID != use.RuleID || (reservation.Direction != "egress" && reservation.Direction != "ingress") {
		return "", ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := reserveAuthorizedUsageTx(ctx, tx, reservation, use)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func reserveAuthorizedUsageTx(ctx context.Context, tx *sql.Tx, reservation UsageReservation, use AuthorizationUse) (string, error) {
	var err error
	var priorAuth, priorReserved, priorState string
	err = tx.QueryRowContext(ctx, `SELECT authorization_id,reserved_bytes,state FROM authorization_usage_holds WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&priorAuth, &priorReserved, &priorState)
	if err == nil {
		prior, parseErr := strconv.ParseUint(priorReserved, 10, 64)

		if parseErr != nil || priorAuth != use.AuthorizationID || prior != reservation.Reserved || priorState != "reserved" {
			return "", ErrUsageBatchConflict
		}
		var bootID, snapshotID, flowKey, ruleID, direction, reservationAmount, resolution string
		err = tx.QueryRowContext(ctx, `SELECT boot_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state FROM usage_reservations WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&bootID, &snapshotID, &flowKey, &ruleID, &direction, &reservationAmount, &resolution)
		if err != nil || bootID != reservation.BootID || snapshotID != reservation.SnapshotID || flowKey != reservation.FlowKey || ruleID != reservation.RuleID || direction != reservation.Direction || reservationAmount != strconv.FormatUint(reservation.Reserved, 10) || resolution != "reserved" {
			return "", ErrUsageBatchConflict
		}
		if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
			return "", err
		}
		return priorAuth, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	record, err := checkAuthorizationTx(ctx, tx, use)
	if err != nil {
		return "", err
	}
	effectiveID := use.AuthorizationID
	if record.Kind != "credit_grant" || reservation.Reserved > record.AmountBytes {
		return "", ErrAuthorizationDenied
	}
	var sourceID, remainingText string
	err = tx.QueryRowContext(ctx, `SELECT source_id,remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, use.NodeID, effectiveID).Scan(&sourceID, &remainingText)
	if err != nil || sourceID != record.SourceID {
		return "", ErrAuthorizationDenied
	}
	remaining, err := strconv.ParseUint(remainingText, 10, 64)
	if err != nil || remaining < reservation.Reserved {
		return "", ErrAuthorizationDenied
	}
	if use.SessionID != "" {
		if err := consumeRuntimeRate(ctx, tx, use, reservation.Reserved); err != nil {
			return "", err
		}
	}
	if err := insertUsageReservationTx(ctx, tx, reservation); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_balances SET remaining_bytes=?,updated_at=? WHERE node_id=? AND authorization_id=? AND source_id=?`, strconv.FormatUint(remaining-reservation.Reserved, 10), time.Now().UTC().Format(time.RFC3339Nano), use.NodeID, effectiveID, record.SourceID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_usage_holds(node_id,batch_id,authorization_id,source_id,reserved_bytes,state) VALUES(?,?,?,?,?,?)`, use.NodeID, reservation.BatchID, effectiveID, record.SourceID, strconv.FormatUint(reservation.Reserved, 10), "reserved"); err != nil {
		return "", err
	}
	return effectiveID, nil
}

func insertUsageReservationTx(ctx context.Context, tx *sql.Tx, r UsageReservation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_reservations(node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.NodeID, r.BootID, r.BatchID, r.SnapshotID, r.FlowKey, r.RuleID, r.Direction, strconv.FormatUint(r.Reserved, 10), "reserved", time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) RemainingAuthorizationBytes(ctx context.Context, nodeID, authorizationID string) (uint64, error) {
	var text string
	if err := s.db.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, nodeID, authorizationID).Scan(&text); err != nil {
		return 0, err
	}
	return strconv.ParseUint(text, 10, 64)
}

type AuthorizationBalance struct {
	Initial, Remaining, Committed, Unknown, Reserved uint64
}

// AuditAuthorizationBalance checks conservation for one persisted credit grant.
func (s *Store) AuditAuthorizationBalance(ctx context.Context, nodeID, authorizationID string) (AuthorizationBalance, error) {
	var result AuthorizationBalance
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var initialText, remainingText string
	if err := tx.QueryRowContext(ctx, `SELECT initial_bytes,remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, nodeID, authorizationID).Scan(&initialText, &remainingText); err != nil {
		return result, err
	}
	if result.Initial, err = strconv.ParseUint(initialText, 10, 64); err != nil {
		return result, err
	}
	if result.Remaining, err = strconv.ParseUint(remainingText, 10, 64); err != nil {
		return result, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT h.reserved_bytes,h.state,u.used_bytes FROM authorization_usage_holds h LEFT JOIN usage_reservations u ON u.node_id=h.node_id AND u.batch_id=h.batch_id WHERE h.node_id=? AND h.authorization_id=?`, nodeID, authorizationID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var reservedText, usedText, state string
		if err := rows.Scan(&reservedText, &state, &usedText); err != nil {
			return result, err
		}
		reserved, err := strconv.ParseUint(reservedText, 10, 64)
		if err != nil {
			return result, err
		}
		used, err := strconv.ParseUint(usedText, 10, 64)
		if err != nil {
			return result, err
		}
		switch state {
		case "reserved":
			if err := addAuthorizationAmount(&result.Reserved, reserved, result.Initial); err != nil {
				return result, err
			}
		case "committed":
			if used > reserved {
				return result, ErrUsageBatchConflict
			}
			if err := addAuthorizationAmount(&result.Committed, used, result.Initial); err != nil {
				return result, err
			}
		case "unknown":
			if err := addAuthorizationAmount(&result.Unknown, reserved, result.Initial); err != nil {
				return result, err
			}
		case "returned":
		default:
			return result, ErrUsageBatchConflict
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if result.Remaining > result.Initial || result.Committed > result.Initial-result.Remaining || result.Unknown > result.Initial-result.Remaining-result.Committed || result.Reserved != result.Initial-result.Remaining-result.Committed-result.Unknown {
		return result, fmt.Errorf("store: authorization source balance violates conservation")
	}
	return result, nil
}

// AuditAuthorizationSource verifies the single-source conservation equation
// across outstanding usage holds.
func (s *Store) AuditAuthorizationSource(ctx context.Context, nodeID, sourceID string) (AuthorizationBalance, error) {
	var result AuthorizationBalance
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var initialText string
	if err := tx.QueryRowContext(ctx, `SELECT initial_bytes FROM authorization_source_owners WHERE node_id=? AND source_id=?`, nodeID, sourceID).Scan(&initialText); err != nil {
		return result, err
	}
	if result.Initial, err = strconv.ParseUint(initialText, 10, 64); err != nil {
		return result, err
	}
	balances, err := tx.QueryContext(ctx, `SELECT initial_bytes,remaining_bytes FROM authorization_balances WHERE node_id=? AND source_id=?`, nodeID, sourceID)
	if err != nil {
		return result, err
	}
	var sumInitial uint64
	for balances.Next() {
		var initial, remaining string
		if err := balances.Scan(&initial, &remaining); err != nil {
			balances.Close()
			return result, err
		}
		i, e1 := strconv.ParseUint(initial, 10, 64)
		r, e2 := strconv.ParseUint(remaining, 10, 64)
		if e1 != nil || e2 != nil || r > i {
			balances.Close()
			return result, ErrUsageBatchConflict
		}
		if err := addAuthorizationAmount(&sumInitial, i, result.Initial); err != nil {
			balances.Close()
			return result, err
		}
		if err := addAuthorizationAmount(&result.Remaining, r, result.Initial); err != nil {
			balances.Close()
			return result, err
		}
	}
	err = balances.Err()
	balances.Close()
	if err != nil {
		return result, err
	}
	if sumInitial != result.Initial {
		return result, ErrUsageBatchConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT h.reserved_bytes,h.state,u.used_bytes FROM authorization_usage_holds h LEFT JOIN usage_reservations u ON u.node_id=h.node_id AND u.batch_id=h.batch_id WHERE h.node_id=? AND h.source_id=?`, nodeID, sourceID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var reservedText, usedText, state string
		if err := rows.Scan(&reservedText, &state, &usedText); err != nil {
			return result, err
		}
		reserved, e := strconv.ParseUint(reservedText, 10, 64)
		if e != nil {
			return result, e
		}
		used, e := strconv.ParseUint(usedText, 10, 64)
		if e != nil {
			return result, e
		}
		switch state {
		case "reserved":
			if err := addAuthorizationAmount(&result.Reserved, reserved, result.Initial); err != nil {
				return result, err
			}
		case "committed":
			if used > reserved {
				return result, ErrUsageBatchConflict
			}
			if err := addAuthorizationAmount(&result.Committed, used, result.Initial); err != nil {
				return result, err
			}
		case "unknown":
			if err := addAuthorizationAmount(&result.Unknown, reserved, result.Initial); err != nil {
				return result, err
			}
		case "returned":
		default:
			return result, ErrUsageBatchConflict
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if result.Remaining > result.Initial || result.Committed > result.Initial-result.Remaining || result.Unknown > result.Initial-result.Remaining-result.Committed || result.Reserved != result.Initial-result.Remaining-result.Committed-result.Unknown {
		return result, fmt.Errorf("store: authorization source %s violates conservation", sourceID)
	}
	return result, nil
}

// A source is anchored to a single parent allocation, boot, billing scope and
// signer epoch. Additional grants from that same authenticated Hub allocation
// are independent reservations, not ownership transfers. Only the Hub knows
// the parent's unissued budget and must reserve it before sending each grant.
func checkSourceLineageTx(ctx context.Context, tx *sql.Tx, nodeID string, r AuthorizationRecord) error {
	var id, parent, boot, snapshot, rule, revision, path, epoch, deadline, issuer string
	err := tx.QueryRowContext(ctx, `SELECT a.authorization_id,a.parent_authorization_id,a.boot_id,a.billing_snapshot_id,a.rule_id,a.rule_revision,a.path_generation,a.signer_epoch,a.hard_deadline,a.issuer_id FROM authorization_source_owners o JOIN authorization_records a ON a.node_id=o.node_id AND a.authorization_id=o.authorization_id WHERE o.node_id=? AND o.source_id=?`, nodeID, r.SourceID).Scan(&id, &parent, &boot, &snapshot, &rule, &revision, &path, &epoch, &deadline, &issuer)
	if err != nil {
		return err
	}
	if id == r.AuthorizationID {
		return nil
	}
	rootDeadline, e1 := time.Parse(time.RFC3339Nano, deadline)
	grantDeadline, e2 := time.Parse(time.RFC3339Nano, r.HardDeadline)
	if issuer == "" || issuer != r.IssuerID || r.ParentAuthorizationID == "" || parent != r.ParentAuthorizationID || boot != r.BootID || snapshot != r.BillingSnapshotID || rule != r.RuleID || revision != strconv.FormatUint(r.RuleRevision, 10) || path != strconv.FormatUint(r.PathGeneration, 10) || epoch != strconv.FormatUint(r.SignerEpoch, 10) || e1 != nil || e2 != nil || grantDeadline.After(rootDeadline) {
		return ErrAuthorityHandoffRequired
	}
	return nil
}

func addAuthorizationAmount(total *uint64, amount, limit uint64) error {
	if *total > limit || amount > limit-*total {
		return ErrUsageBatchConflict
	}
	*total += amount
	return nil
}
