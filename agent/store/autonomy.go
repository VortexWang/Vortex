package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const BackendLossGrace = 30 * time.Second

const (
	AutonomyOnline        = "online"
	AutonomyOffline       = "autonomous"
	AutonomyExpired       = "expired"
	AutonomyTimeUncertain = "time_uncertain"
)

var (
	ErrHeartbeatStale            = errors.New("store: backend heartbeat is not newer than the durable heartbeat")
	ErrAuthorityHandoffRequired  = errors.New("store: authority handoff is required before recording a resumed heartbeat")
	ErrAuthorityExpired          = errors.New("store: authorization hard deadline has passed")
	ErrAuthorityTimeUncertain    = errors.New("store: trusted time is required for authorization")
	ErrNewAuthorityInAutonomy    = errors.New("store: autonomy only permits renewal of existing authority")
	ErrSignerEpochClosed         = errors.New("store: signer epoch is closed")
	ErrSignerEpochOpenInAutonomy = errors.New("store: signer epoch must be closed in autonomy")
)

type AutonomyStatus struct {
	Mode                   string
	BackendHeartbeat       time.Time
	AutonomyStartedAt      time.Time
	HardDeadline           time.Time
	SignerEpochOpen        bool
	AllowsNewAuthorization bool
	AllowsExistingRenewal  bool
}

// RecordBackendHeartbeat persists the absolute autonomy deadline with the
// authenticated heartbeat. Callers choose the configured autonomy bound; this
// method never recomputes it during restart or Hub reconnect.
func (s *Store) RecordBackendHeartbeat(ctx context.Context, heartbeat, hardDeadline time.Time) error {
	if heartbeat.IsZero() || hardDeadline.IsZero() || !hardDeadline.After(heartbeat) {
		return fmt.Errorf("store: heartbeat and later hard deadline are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	previous, previousErr := stateValue(ctx, tx, "backend_heartbeat")
	if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
		return previousErr
	}
	mode, modeErr := stateValue(ctx, tx, "authority_mode")
	if modeErr != nil && !errors.Is(modeErr, sql.ErrNoRows) {
		return modeErr
	}
	if previousErr == nil {
		previousHeartbeat, parseErr := time.Parse(time.RFC3339Nano, previous)
		if parseErr != nil {
			return fmt.Errorf("store: invalid durable heartbeat: %w", parseErr)
		}
		if !heartbeat.After(previousHeartbeat) {
			return ErrHeartbeatStale
		}
		if mode == AutonomyOffline || mode == AutonomyExpired {
			return ErrAuthorityHandoffRequired
		}
		previousDeadline := deadlineFromTx(ctx, tx)
		if previousDeadline.IsZero() {
			return fmt.Errorf("store: durable autonomy deadline is missing")
		}
		if !heartbeat.Before(previousDeadline) {
			return ErrAuthorityExpired
		}
		if !heartbeat.Before(previousHeartbeat.Add(BackendLossGrace)) {
			if err := writeAutonomyTransition(ctx, tx, modeOrOnline(mode), AutonomyOffline, previousHeartbeat.Add(BackendLossGrace), previousHeartbeat, previousDeadline); err != nil {
				return err
			}
			if err := putState(ctx, tx, "authority_mode", AutonomyOffline, previousHeartbeat.Add(BackendLossGrace)); err != nil {
				return err
			}
			if err := putState(ctx, tx, "signer_epoch_open", "false", previousHeartbeat.Add(BackendLossGrace)); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			return ErrAuthorityHandoffRequired
		}
	}
	from := modeOrOnline(mode)
	if err := putState(ctx, tx, "backend_heartbeat", heartbeat.UTC().Format(time.RFC3339Nano), heartbeat); err != nil {
		return err
	}
	if err := putState(ctx, tx, "autonomy_deadline", hardDeadline.UTC().Format(time.RFC3339Nano), heartbeat); err != nil {
		return err
	}
	if err := putState(ctx, tx, "authority_mode", AutonomyOnline, heartbeat); err != nil {
		return err
	}
	if err := putState(ctx, tx, "signer_epoch_open", "true", heartbeat); err != nil {
		return err
	}
	if from != AutonomyOnline {
		if err := writeAutonomyTransition(ctx, tx, from, AutonomyOnline, heartbeat, heartbeat, hardDeadline); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CurrentAutonomyStatus evaluates only persisted absolute times. An untrusted
// clock denies new authority without moving or extending the durable deadline.
func (s *Store) CurrentAutonomyStatus(ctx context.Context, now time.Time, trustedClock bool) (AutonomyStatus, error) {
	if now.IsZero() {
		return AutonomyStatus{}, fmt.Errorf("store: current time is required")
	}
	heartbeatText, err := s.AgentState(ctx, "backend_heartbeat")
	if err != nil {
		return AutonomyStatus{}, err
	}
	deadlineText, err := s.AgentState(ctx, "autonomy_deadline")
	if err != nil {
		return AutonomyStatus{}, err
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, heartbeatText)
	if err != nil {
		return AutonomyStatus{}, fmt.Errorf("store: invalid durable heartbeat: %w", err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, deadlineText)
	if err != nil || !deadline.After(heartbeat) {
		return AutonomyStatus{}, fmt.Errorf("store: invalid durable autonomy deadline")
	}
	status := AutonomyStatus{BackendHeartbeat: heartbeat, HardDeadline: deadline}
	if !trustedClock {
		status.Mode = AutonomyTimeUncertain
		return status, nil
	}
	mode, modeErr := s.AgentState(ctx, "authority_mode")
	if modeErr != nil && !errors.Is(modeErr, sql.ErrNoRows) {
		return AutonomyStatus{}, modeErr
	}
	if errors.Is(modeErr, sql.ErrNoRows) {
		mode = AutonomyOnline
	}
	switch mode {
	case AutonomyExpired:
		status.Mode = AutonomyExpired
	case AutonomyOffline:
		if !now.Before(deadline) {
			if err := s.transitionAutonomy(ctx, AutonomyOffline, AutonomyExpired, now, heartbeat, deadline); err != nil {
				return AutonomyStatus{}, err
			}
			mode = AutonomyExpired
		}
		status.Mode = mode
		status.AutonomyStartedAt = heartbeat.Add(BackendLossGrace)
	case AutonomyOnline:
		if !now.Before(deadline) {
			if !now.Before(heartbeat.Add(BackendLossGrace)) {
				if err := s.transitionAutonomy(ctx, AutonomyOnline, AutonomyOffline, heartbeat.Add(BackendLossGrace), heartbeat, deadline); err != nil {
					return AutonomyStatus{}, err
				}
				if err := s.transitionAutonomy(ctx, AutonomyOffline, AutonomyExpired, now, heartbeat, deadline); err != nil {
					return AutonomyStatus{}, err
				}
			} else if err := s.transitionAutonomy(ctx, AutonomyOnline, AutonomyExpired, now, heartbeat, deadline); err != nil {
				return AutonomyStatus{}, err
			}
			status.Mode = AutonomyExpired
		} else if !now.Before(heartbeat.Add(BackendLossGrace)) {
			if err := s.transitionAutonomy(ctx, AutonomyOnline, AutonomyOffline, now, heartbeat, deadline); err != nil {
				return AutonomyStatus{}, err
			}
			status.Mode = AutonomyOffline
			status.AutonomyStartedAt = heartbeat.Add(BackendLossGrace)
		} else {
			status.Mode = AutonomyOnline
		}
	default:
		return AutonomyStatus{}, fmt.Errorf("store: invalid durable authority mode %q", mode)
	}
	status.SignerEpochOpen, err = s.signerEpochOpen(ctx)
	if err != nil {
		return AutonomyStatus{}, err
	}
	if status.Mode != AutonomyOnline && status.SignerEpochOpen {
		return AutonomyStatus{}, ErrSignerEpochOpenInAutonomy
	}
	if status.Mode == AutonomyOnline {
		status.AllowsNewAuthorization = status.SignerEpochOpen
		status.AllowsExistingRenewal = true
	} else if status.Mode == AutonomyOffline && now.Before(deadline) {
		status.AllowsExistingRenewal = true
	}
	return status, nil
}

func (s *Store) CheckAuthorization(ctx context.Context, now time.Time, trustedClock, existingRule, existingShare, existingDelegation bool) error {
	status, err := s.CurrentAutonomyStatus(ctx, now, trustedClock)
	if err != nil {
		return err
	}
	if status.Mode == AutonomyTimeUncertain {
		return ErrAuthorityTimeUncertain
	}
	if status.Mode == AutonomyExpired {
		return ErrAuthorityExpired
	}
	if status.Mode == AutonomyOnline && !status.SignerEpochOpen {
		return ErrSignerEpochClosed
	}
	if status.Mode == AutonomyOffline && status.SignerEpochOpen {
		return ErrSignerEpochOpenInAutonomy
	}
	if status.Mode == AutonomyOffline && !(existingRule && existingShare && existingDelegation) {
		return ErrNewAuthorityInAutonomy
	}
	return nil
}

func (s *Store) transitionAutonomy(ctx context.Context, from, to string, at, heartbeat, deadline time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := stateValue(ctx, tx, "authority_mode")
	if errors.Is(err, sql.ErrNoRows) {
		current = AutonomyOnline
	} else if err != nil {
		return err
	}
	if current != from {
		return fmt.Errorf("store: authority mode raced from %s to %s", from, current)
	}
	currentHeartbeat, err := stateValue(ctx, tx, "backend_heartbeat")
	if err != nil {
		return err
	}
	currentDeadline, err := stateValue(ctx, tx, "autonomy_deadline")
	if err != nil {
		return err
	}
	if currentHeartbeat != heartbeat.UTC().Format(time.RFC3339Nano) || currentDeadline != deadline.UTC().Format(time.RFC3339Nano) {
		return errors.New("store: heartbeat or deadline changed during autonomy evaluation")
	}
	if err := putState(ctx, tx, "authority_mode", to, at); err != nil {
		return err
	}
	if to != AutonomyOnline {
		if err := putState(ctx, tx, "signer_epoch_open", "false", at); err != nil {
			return err
		}
	}
	if err := writeAutonomyTransition(ctx, tx, from, to, at, heartbeat, deadline); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) signerEpochOpen(ctx context.Context) (bool, error) {
	value, err := s.AgentState(ctx, "signer_epoch_open")
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true", nil
}

func stateValue(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key=?`, key).Scan(&value)
	return value, err
}

func putState(ctx context.Context, tx *sql.Tx, key, value string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_state(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, at.UTC().Format(time.RFC3339Nano))
	return err
}

func writeAutonomyTransition(ctx context.Context, tx *sql.Tx, from, to string, at, heartbeat, deadline time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO autonomy_transitions(from_mode,to_mode,occurred_at,backend_heartbeat,hard_deadline) VALUES(?,?,?,?,?)`, from, to, at.UTC().Format(time.RFC3339Nano), heartbeat.UTC().Format(time.RFC3339Nano), deadline.UTC().Format(time.RFC3339Nano))
	return err
}

func modeOrOnline(mode string) string {
	if mode == "" {
		return AutonomyOnline
	}
	return mode
}

func deadlineFromTx(ctx context.Context, tx *sql.Tx) time.Time {
	value, err := stateValue(ctx, tx, "autonomy_deadline")
	if err != nil {
		return time.Time{}
	}
	deadline, _ := time.Parse(time.RFC3339Nano, value)
	return deadline
}
