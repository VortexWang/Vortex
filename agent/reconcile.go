package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"vortex/agent/store"
)

var (
	ErrStaleRevision     = errors.New("agent: stale configuration revision")
	ErrConfigConflict    = errors.New("agent: configuration transaction conflict")
	ErrActivationUnknown = errors.New("agent: configuration activation unknown")
)

type ObjectResult = store.ObjectResult

type Applier interface {
	Prepare([16]byte, uint64) (any, error)
	Commit([16]byte, bool) (any, error)
	Abort([16]byte) (any, error)
}

// Reconciler persists the transaction outcome before exposing it to control callers.
type Reconciler struct {
	Store *store.Store
	Apply Applier
	mu    sync.Mutex
}

func (r *Reconciler) ApplyConfig(ctx context.Context, txID string, version uint64, generation uint64, commitUnknown bool) error {
	if r == nil {
		return fmt.Errorf("agent: reconciler dependencies are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Store == nil || r.Apply == nil {
		return fmt.Errorf("agent: reconciler dependencies are required")
	}
	if len(txID) != 32 {
		return fmt.Errorf("agent: transaction id must be 32 hexadecimal characters")
	}
	decoded, err := hex.DecodeString(txID)
	if err != nil || strings.TrimSpace(txID) != txID {
		return fmt.Errorf("agent: invalid transaction id")
	}
	var id [16]byte
	copy(id[:], decoded)
	if previous, err := r.Store.GetConfigResult(ctx, txID); err == nil {
		if previous.Version != version {
			return ErrConfigConflict
		}
		if previous.FencingGeneration != generation {
			return ErrConfigConflict
		}
		switch previous.State {
		case "active":
			return nil
		case "activation-unknown", "committing", "commit-requested":
			return ErrActivationUnknown
		case "released", "aborted", "prepare-failed", "commit-failed":
			return ErrConfigConflict
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Even callers outside the control decoder must durably claim the
	// fencing generation before touching Engine state. An unresolved commit
	// blocks new transactions through the same store-level guard.
	if _, err := r.Store.GetConfigResult(ctx, txID); errors.Is(err, sql.ErrNoRows) {
		expected := uint64(0)
		if raw, err := r.Store.AgentState(ctx, "applied_version"); err == nil {
			expected, err = strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		payload := []byte(fmt.Sprintf("legacy-apply:%s:%d:%d", txID, version, generation))
		digest := sha256.Sum256(payload)
		if _, err := r.Store.SaveConfigRequest(ctx, store.ConfigResult{TransactionID: txID, Version: version, FencingGeneration: generation, ExpectedVersion: expected, Payload: payload, PayloadSHA256: digest[:]}); err != nil {
			if errors.Is(err, store.ErrConfigVersionStale) {
				return ErrStaleRevision
			}
			return err
		}
	}
	if current, err := r.Store.AgentState(ctx, "applied_version"); err == nil {
		applied, parseErr := strconv.ParseUint(current, 10, 64)
		if parseErr != nil {
			return fmt.Errorf("agent: stored applied version invalid")
		}
		if version < applied {
			return ErrStaleRevision
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if executor, ok := r.Apply.(interface{ ExecutionID() string }); ok {
		if err := r.Store.BindActivationExecutor(ctx, txID, executor.ExecutionID()); err != nil {
			return err
		}
	}
	if preparer, ok := r.Apply.(interface {
		PrepareConfig([16]byte, uint64, []byte) error
	}); ok {
		record, err := r.Store.GetConfigResult(ctx, txID)
		if err != nil {
			return err
		}
		if err = preparer.PrepareConfig(id, generation, record.Payload); err != nil {
			if saveErr := r.Store.SaveConfigResult(ctx, txID, version, "prepare-failed", err.Error()); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
	}
	if _, err := r.Apply.Prepare(id, generation); err != nil {
		if saveErr := r.Store.SaveConfigResult(ctx, txID, version, "prepare-failed", err.Error()); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	if err := r.Store.SaveConfigResult(ctx, txID, version, "prepared", ""); err != nil {
		_, _ = r.Apply.Abort(id)
		return err
	}
	if err := r.Store.SaveConfigResult(ctx, txID, version, "committing", ""); err != nil {
		_, _ = r.Apply.Abort(id)
		return err
	}
	result, err := r.Apply.Commit(id, commitUnknown)
	var objectResults []ObjectResult
	if results, ok := result.([]ObjectResult); ok && len(results) > 0 {
		if err := validateObjectResults(results); err != nil {
			return err
		}
		objectResults = results
		if saveErr := r.Store.SaveObjectResults(ctx, txID, results); saveErr != nil {
			return saveErr
		}
	}
	if errors.Is(err, ErrActivationPending) {
		if saveErr := r.Store.SaveConfigResult(ctx, txID, version, "prepared", ""); saveErr != nil {
			return saveErr
		}
		return err
	}
	if err != nil {
		state := "activation-unknown"
		if errors.Is(err, ErrConfigConflict) {
			state = "commit-failed"
		}
		if saveErr := r.Store.SaveConfigResult(ctx, txID, version, state, err.Error()); saveErr != nil {
			return saveErr
		}
		return err
	}
	return r.Store.FinalizeConfigResult(ctx, txID, version, objectResults)
}
