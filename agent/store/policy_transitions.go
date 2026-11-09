package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

// PolicySnapshot is the approved immutable authorization1.2.0 billing binding.
// It describes billing; funded credit/runtime/share records remain mandatory.
type PolicySnapshot struct {
	ID               string `json:"snapshot_id"`
	RuleID           string `json:"rule_id"`
	NodeID           string `json:"execution_node_id"`
	PathGeneration   uint64 `json:"path_generation"`
	Revision         uint64 `json:"policy_revision"`
	CoefficientMilli uint64 `json:"coefficient_milli"`
}
type PolicyTransition struct {
	ID              string         `json:"transition_id"`
	RuleID          string         `json:"rule_id"`
	PathGeneration  uint64         `json:"path_generation"`
	Old             PolicySnapshot `json:"old_snapshot"`
	New             PolicySnapshot `json:"new_snapshot"`
	EffectiveAt     time.Time      `json:"effective_at"`
	PreissueSeconds uint64         `json:"preissue_seconds"`
	GuardSeconds    uint64         `json:"guard_seconds"`
	FutureSourceID  string         `json:"future_source_id"`
	FutureCreditID  string         `json:"future_credit_authorization_id"`
	FutureRuntimeID string         `json:"future_runtime_authorization_id"`
	FutureShareID   string         `json:"future_share_authorization_id"`
}

func ValidatePolicyTransition(p PolicyTransition) error {
	for _, id := range []string{p.ID, p.Old.ID, p.New.ID, p.Old.NodeID, p.New.NodeID, p.FutureSourceID, p.FutureCreditID, p.FutureRuntimeID, p.FutureShareID} {
		if !validIssuerUUID(id) {
			return ErrAuthorizationDenied
		}
	}
	if p.Old.ID == p.New.ID || p.RuleID == "" || len(p.RuleID) > 128 || p.PathGeneration == 0 || p.PathGeneration > math.MaxInt64 || p.New.Revision <= p.Old.Revision || p.EffectiveAt.IsZero() || p.EffectiveAt.Location() != time.UTC || p.PreissueSeconds > 86400 || p.GuardSeconds > 300 {
		return ErrAuthorizationDenied
	}
	for _, s := range []PolicySnapshot{p.Old, p.New} {
		if s.RuleID != p.RuleID || s.PathGeneration != p.PathGeneration || s.Revision == 0 || s.Revision > math.MaxInt64 || s.CoefficientMilli == 0 || s.CoefficientMilli > 1000000000 {
			return ErrAuthorizationDenied
		}
	}
	if p.Old.NodeID != p.New.NodeID {
		return ErrAuthorizationDenied
	}
	return nil
}
func (s *Store) migratePolicyTransitions() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS config_share_holds(transaction_id TEXT NOT NULL REFERENCES config_results(transaction_id),node_id TEXT NOT NULL,boot_id TEXT NOT NULL,share_id TEXT NOT NULL,revision TEXT NOT NULL,generation TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('held','released')),PRIMARY KEY(transaction_id,node_id,share_id,revision));
 CREATE TABLE IF NOT EXISTS policy_snapshots(node_id TEXT NOT NULL,snapshot_id TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,PRIMARY KEY(node_id,snapshot_id));
 CREATE TABLE IF NOT EXISTS policy_transitions(node_id TEXT NOT NULL,boot_id TEXT NOT NULL,transition_id TEXT NOT NULL,issuer_id TEXT NOT NULL,rule_id TEXT NOT NULL,path_generation TEXT NOT NULL,effective_at TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,PRIMARY KEY(node_id,transition_id),UNIQUE(node_id,boot_id,rule_id,path_generation,effective_at));
 INSERT INTO schema_meta(key,value) VALUES('policy_transition_version','1') ON CONFLICT(key) DO NOTHING;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func persistPolicyTransitionsTx(ctx context.Context, tx storeTransaction, u AuthorizationUpdate) error {
	if len(u.PolicyTransitions) > 64 {
		return ErrAuthorizationDenied
	}
	seen := map[string]bool{}
	for _, p := range u.PolicyTransitions {
		if seen[p.ID] {
			return ErrAuthorizationDenied
		}
		seen[p.ID] = true
		if err := ValidatePolicyTransition(p); err != nil {
			return err
		}
		if p.New.NodeID != u.NodeID || p.PathGeneration != u.PathGeneration || u.BackendHeartbeat.IsZero() {
			return ErrAuthorizationDenied
		}
		raw, _ := json.Marshal(p)
		sum := sha256.Sum256(raw)
		var previous []byte
		var boot, issuer string
		err := tx.QueryRowContext(ctx, `SELECT payload,boot_id,issuer_id FROM policy_transitions WHERE node_id=? AND transition_id=?`, u.NodeID, p.ID).Scan(&previous, &boot, &issuer)
		if err == nil {
			if !bytes.Equal(previous, raw) || boot != u.BootID || issuer != u.IssuerID {
				return ErrAuthorizationUpdateConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		mode, err := stateValue(ctx, tx, "authority_mode")
		if err != nil {
			return err
		}
		if mode != AutonomyOnline {
			return ErrNewAuthorityInAutonomy
		}
		var oldCount int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM authorization_records WHERE node_id=? AND boot_id=? AND issuer_id=? AND kind='credit_grant' AND rule_id=? AND path_generation=? AND billing_snapshot_id=?`, u.NodeID, u.BootID, u.IssuerID, p.RuleID, policyUint(p.PathGeneration), p.Old.ID).Scan(&oldCount); err != nil || oldCount == 0 {
			return ErrAuthorizationDenied
		}
		// A delayed old proposal must not insert before the newest durable boundary.
		var latest sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT effective_at FROM policy_transitions WHERE node_id=? AND rule_id=? AND path_generation=? ORDER BY rowid DESC LIMIT 1)`, u.NodeID, p.RuleID, policyUint(p.PathGeneration)).Scan(&latest); err != nil {
			return err
		}
		if latest.Valid {
			at, e := time.Parse(time.RFC3339Nano, latest.String)
			if e != nil || !p.EffectiveAt.After(at) {
				return ErrAuthorizationGenerationStale
			}
		}
		if u.BackendHeartbeat.Before(p.EffectiveAt.Add(-time.Duration(p.PreissueSeconds) * time.Second)) {
			return ErrAuthorityTimeUncertain
		}
		// Future source ownership is separate; never reinterpret old-period funds.
		grant, state, err := readAuthorizationTx(ctx, tx, u.NodeID, p.FutureCreditID)
		if err != nil || state != "active" || grant.SourceID != p.FutureSourceID || grant.BillingSnapshotID != p.New.ID || grant.Kind != "credit_grant" || grant.AmountBytes == 0 {
			return ErrAuthorizationDenied
		}
		for _, entry := range []struct{ id, kind string }{{p.FutureCreditID, "credit_grant"}, {p.FutureRuntimeID, "runtime_lease"}, {p.FutureShareID, "share_lease"}} {
			r, state, err := readAuthorizationTx(ctx, tx, u.NodeID, entry.id)
			if err != nil || state != "active" || r.Kind != entry.kind || r.BootID != u.BootID || r.IssuerID != u.IssuerID || r.SignerEpoch != u.SignerEpoch || r.RuleID != p.RuleID || r.PathGeneration != p.PathGeneration {
				return ErrAuthorizationDenied
			}
			start, e1 := time.Parse(time.RFC3339Nano, r.NotBefore)
			end, e2 := time.Parse(time.RFC3339Nano, r.ExpiresAt)
			if e1 != nil || e2 != nil || start.Before(p.EffectiveAt) || !end.After(p.EffectiveAt) {
				return ErrAuthorizationDenied
			}
			if err = checkAuthorizationShareTx(ctx, tx, u.NodeID, r, true); err != nil {
				return err
			}
		}
		var oldSources int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM authorization_records WHERE node_id=? AND kind='credit_grant' AND billing_snapshot_id=? AND source_id=?`, u.NodeID, p.Old.ID, p.FutureSourceID).Scan(&oldSources); err != nil || oldSources != 0 {
			return ErrAuthorizationDenied
		}
		for _, binding := range []PolicySnapshot{p.Old, p.New} {
			value, _ := json.Marshal(binding)
			hash := sha256.Sum256(value)
			var prior []byte
			err = tx.QueryRowContext(ctx, `SELECT payload FROM policy_snapshots WHERE node_id=? AND snapshot_id=?`, u.NodeID, binding.ID).Scan(&prior)
			if err == nil {
				if !bytes.Equal(prior, value) {
					return ErrAuthorizationUpdateConflict
				}
			} else if errors.Is(err, sql.ErrNoRows) {
				if _, err = tx.ExecContext(ctx, `INSERT INTO policy_snapshots VALUES(?,?,?,?)`, u.NodeID, binding.ID, value, hash[:]); err != nil {
					return err
				}
			} else {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO policy_transitions VALUES(?,?,?,?,?,?,?,?,?)`, u.NodeID, u.BootID, p.ID, u.IssuerID, p.RuleID, policyUint(p.PathGeneration), p.EffectiveAt.Format(time.RFC3339Nano), raw, sum[:]); err != nil {
			return err
		}
	}
	return nil
}

// policySnapshotTx selects only for new sessions. Established sessions use
// their original immutable snapshot and cannot merge into future-period funds.
func policySnapshotTx(ctx context.Context, tx storeTransaction, scope AuthorizationUse) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT payload,payload_sha256 FROM policy_transitions WHERE node_id=? AND boot_id=? AND rule_id=? AND path_generation=? AND (unixepoch(effective_at)>=unixepoch(?) OR rowid=(SELECT rowid FROM policy_transitions WHERE node_id=? AND boot_id=? AND rule_id=? AND path_generation=? AND unixepoch(effective_at)<unixepoch(?) ORDER BY rowid DESC LIMIT 1)) ORDER BY rowid LIMIT 66`, scope.NodeID, scope.BootID, scope.RuleID, policyUint(scope.PathGeneration), scope.Earliest.UTC().Format(time.RFC3339Nano), scope.NodeID, scope.BootID, scope.RuleID, policyUint(scope.PathGeneration), scope.Earliest.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	var transitions []PolicyTransition
	for rows.Next() {
		var raw, hash []byte
		if err = rows.Scan(&raw, &hash); err != nil {
			rows.Close()
			return "", err
		}
		sum := sha256.Sum256(raw)
		var p PolicyTransition
		if !bytes.Equal(sum[:], hash) || json.Unmarshal(raw, &p) != nil || ValidatePolicyTransition(p) != nil {
			rows.Close()
			return "", ErrAuthorizationDenied
		}
		transitions = append(transitions, p)
		if len(transitions) > 65 {
			rows.Close()
			return "", ErrAuthorizationDenied
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	var selected string
	for _, p := range transitions {
		if !scope.TrustedTime || scope.Earliest.IsZero() || scope.Latest.Before(scope.Earliest) {
			return "", ErrAuthorityTimeUncertain
		}
		var revoked int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM authorization_revocations WHERE node_id=? AND authorization_id=?`, scope.NodeID, p.ID).Scan(&revoked); err != nil {
			return "", err
		}
		if revoked != 0 {
			return "", ErrAuthorizationDenied
		}
		if scope.Earliest.Before(p.EffectiveAt) {
			// No new old-period session may cross the conservative guard boundary.
			if !scope.Latest.Before(p.EffectiveAt.Add(-time.Duration(p.GuardSeconds) * time.Second)) {
				return "", ErrAuthorityTimeUncertain
			}
			if selected == "" {
				selected = p.Old.ID
			}
			break
		}
		selected = p.New.ID
	}
	return selected, nil
}

func policyUint(n uint64) string { return strconv.FormatUint(n, 10) }

func policyAuthorityIDsTx(ctx context.Context, tx storeTransaction, scope AuthorizationUse) (map[string]string, error) {
	ids := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT payload,payload_sha256 FROM policy_transitions WHERE node_id=? AND boot_id=? AND rule_id=? AND path_generation=? AND json_extract(CAST(payload AS TEXT),'$.new_snapshot.snapshot_id')=? ORDER BY rowid DESC LIMIT 1`, scope.NodeID, scope.BootID, scope.RuleID, policyUint(scope.PathGeneration), scope.BillingSnapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, digest []byte
		var p PolicyTransition
		if err = rows.Scan(&raw, &digest); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &p) != nil || ValidatePolicyTransition(p) != nil {
			return nil, ErrAuthorizationDenied
		}
		if p.New.ID == scope.BillingSnapshotID {
			ids["credit_grant"] = p.FutureCreditID
			ids["runtime_lease"] = p.FutureRuntimeID
			ids["share_lease"] = p.FutureShareID
			break
		}
	}
	return ids, rows.Err()
}
