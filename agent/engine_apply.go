package agent

import (
	"errors"

	"vortex/engine/apply"
)

// EngineApplier adapts the Engine's process-local transaction manager.
// Durable recovery and actual route configuration are owned by the Agent runtime.
type EngineApplier struct {
	Manager *apply.Manager
}

func (a EngineApplier) ExecutionID() string {
	if a.Manager == nil {
		return ""
	}
	return a.Manager.InstanceID()
}

func (a EngineApplier) Query(id [16]byte) (apply.Transaction, bool) {
	if a.Manager == nil {
		return apply.Transaction{}, false
	}
	return a.Manager.Query(id)
}

func (a EngineApplier) Prepare(id [16]byte, generation uint64) (any, error) {
	if a.Manager == nil {
		return nil, ErrConfigConflict
	}
	result, err := a.Manager.Prepare(id, generation)
	if errors.Is(err, apply.ErrConflict) {
		return nil, ErrConfigConflict
	}
	return result, err
}

func (a EngineApplier) Commit(id [16]byte, unknown bool) (any, error) {
	if a.Manager == nil {
		return nil, ErrConfigConflict
	}
	result, err := a.Manager.Commit(id, unknown)
	if errors.Is(err, apply.ErrUnknown) {
		return result, ErrActivationUnknown
	}
	if errors.Is(err, apply.ErrConflict) {
		return result, ErrConfigConflict
	}
	return result, err
}

func (a EngineApplier) Abort(id [16]byte) (any, error) {
	if a.Manager == nil {
		return nil, ErrConfigConflict
	}
	result, err := a.Manager.Abort(id)
	if errors.Is(err, apply.ErrConflict) {
		return result, ErrConfigConflict
	}
	return result, err
}
