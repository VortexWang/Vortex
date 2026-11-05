package agent

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"vortex/agent/store"
)

type Applier interface {
	Prepare([16]byte, uint64) (any, error)
	Commit([16]byte, bool) (any, error)
	Abort([16]byte) (any, error)
}

// Reconciler persists the transaction outcome before exposing it to control callers.
type Reconciler struct {
	Store *store.Store
	Apply Applier
}

func (r Reconciler) ApplyConfig(ctx context.Context, txID string, version uint64, generation uint64, commitUnknown bool) error {
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
	if _, err := r.Apply.Prepare(id, generation); err != nil {
		_ = r.Store.SaveConfigResult(ctx, txID, version, "prepare-failed", err.Error())
		return err
	}
	if _, err := r.Apply.Commit(id, commitUnknown); err != nil {
		state := "activation-unknown"
		if !commitUnknown {
			state = "commit-failed"
		}
		if saveErr := r.Store.SaveConfigResult(ctx, txID, version, state, err.Error()); saveErr != nil {
			return saveErr
		}
		return err
	}
	return r.Store.SaveConfigResult(ctx, txID, version, "active", "")
}
