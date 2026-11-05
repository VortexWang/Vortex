package agent

import (
	"context"
	"encoding/hex"
	"fmt"
	"vortex/engine/apply"
)

type executionQuerier interface {
	Query([16]byte) (apply.Transaction, bool)
	ExecutionID() string
}

// RecoverConfig asks the same execution boundary for retained transaction
// state. A freshly created manager's empty map is uncertainty, not stop proof.
func (r *Reconciler) RecoverConfig(ctx context.Context, id string) error {
	if r == nil || r.Store == nil || r.Apply == nil {
		return ErrConfigConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.Store.GetConfigResult(ctx, id)
	if err != nil {
		return err
	}
	if record.FencingGeneration == 0 {
		return ErrConfigConflict
	}
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != 16 {
		return ErrConfigConflict
	}
	var txid [16]byte
	copy(txid[:], raw)
	q, ok := r.Apply.(executionQuerier)
	if !ok {
		return fmt.Errorf("agent: execution query unavailable")
	}
	sameExecutor, err := r.Store.ActivationExecutorMatches(ctx, id, q.ExecutionID())
	if err != nil {
		return err
	}
	result, found := q.Query(txid)
	found = found && sameExecutor
	state := "missing"
	if found {
		if result.ID != txid || result.Generation != record.FencingGeneration {
			return ErrConfigConflict
		}
		state = string(result.State)
	}
	if err := r.Store.ObserveActivation(ctx, id, record.FencingGeneration, state); err != nil {
		return err
	}
	if state == "active" || state == "aborted" || state == "released" {
		return nil
	}
	return ErrActivationUnknown
}
