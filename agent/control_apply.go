package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (r *Reconciler) ApplyRuntimeConfig(ctx context.Context, authenticatedNodeID string, update *controlv1.RuntimeConfigUpdate) *controlv1.ApplyResult {
	result := &controlv1.ApplyResult{State: "rejected"}
	if update == nil {
		result.ErrorCode = "invalid_config"
		return result
	}
	result.Version = update.GetVersion()
	result.TransactionId = append([]byte(nil), update.GetTransactionId()...)
	result.FencingGeneration = update.GetFencingGeneration()
	if !canonicalUUID(authenticatedNodeID) {
		result.ErrorCode = "identity_mismatch"
		return result
	}
	if update.GetVersion() == 0 || update.GetFencingGeneration() == 0 || len(update.GetTransactionId()) != 16 || len(update.GetPayloadSha256()) != sha256.Size {
		result.ErrorCode = "invalid_config"
		return result
	}
	var txBytes [16]byte
	copy(txBytes[:], update.GetTransactionId())
	if txBytes == ([16]byte{}) {
		result.ErrorCode = "invalid_transaction_id"
		return result
	}
	digest := sha256.Sum256(update.GetPayload())
	if !equalBytes(digest[:], update.GetPayloadSha256()) {
		result.ErrorCode = "payload_digest_mismatch"
		return result
	}
	if err := validateRuntimeConfig(update.GetPayload(), authenticatedNodeID, update.GetVersion()); err != nil {
		result.ErrorCode = stableConfigError(err)
		return result
	}
	if r == nil || r.Store == nil || r.Apply == nil {
		result.ErrorCode = "prepare_failed"
		return result
	}
	txID := hex.EncodeToString(update.GetTransactionId())
	request := store.ConfigResult{
		TransactionID: txID, Version: update.GetVersion(),
		FencingGeneration: update.GetFencingGeneration(), ExpectedVersion: update.GetExpectedVersion(),
		Payload: append([]byte(nil), update.GetPayload()...), PayloadSHA256: append([]byte(nil), update.GetPayloadSha256()...),
	}
	prior, err := r.Store.SaveConfigRequest(ctx, request)
	if err != nil {
		result.ErrorCode = storeConfigError(err)
		return result
	}
	if prior.State == "active" {
		return r.readApplyResult(ctx, prior)
	}
	if err := r.ApplyConfig(ctx, txID, update.GetVersion(), update.GetFencingGeneration(), false); err != nil {
		stored, readErr := r.Store.GetConfigResult(ctx, txID)
		if readErr == nil {
			result.State = wireApplyState(stored.State)
			result.ErrorCode = stateErrorCode(stored.State)
			if objects, objectErr := r.Store.GetObjectResults(ctx, txID); objectErr == nil {
				result.Objects = controlObjectResults(objects)
			}
			return result
		}
		result.ErrorCode = storeConfigError(err)
		return result
	}
	stored, err := r.Store.GetConfigResult(ctx, txID)
	if err != nil {
		result.ErrorCode = "commit_failed"
		return result
	}
	return r.readApplyResult(ctx, stored)
}

func (r *Reconciler) readApplyResult(ctx context.Context, stored store.ConfigResult) *controlv1.ApplyResult {
	result := &controlv1.ApplyResult{
		Version: stored.Version, State: wireApplyState(stored.State),
		FencingGeneration: stored.FencingGeneration,
	}
	result.TransactionId, _ = hex.DecodeString(stored.TransactionID)
	if objects, err := r.Store.GetObjectResults(ctx, stored.TransactionID); err == nil {
		result.Objects = controlObjectResults(objects)
	}
	if stored.State != "active" {
		result.ErrorCode = stateErrorCode(stored.State)
	}
	return result
}

func controlObjectResults(objects []ObjectResult) []*controlv1.ObjectResult {
	if len(objects) > 10000 {
		return nil
	}
	results := make([]*controlv1.ObjectResult, 0, len(objects))
	for _, object := range objects {
		if !validObjectResult(object) {
			return nil
		}
		results = append(results, &controlv1.ObjectResult{ObjectId: object.ObjectID, State: object.State, Reason: object.Reason})
	}
	return results
}

func validateObjectResults(objects []ObjectResult) error {
	if len(objects) > 10000 {
		return fmt.Errorf("agent: per-object result count exceeds contract limit")
	}
	for _, object := range objects {
		if !validObjectResult(object) {
			return fmt.Errorf("agent: invalid per-object result; commit remains unresolved")
		}
	}
	return nil
}

func validObjectResult(object ObjectResult) bool {
	if len(object.ObjectID) == 0 || len(object.ObjectID) > 128 || !utf8.ValidString(object.ObjectID) || len(object.State) > 64 || len(object.Reason) > 64 {
		return false
	}
	switch object.State {
	case "active", "prepared", "rejected", "aborted", "activation_unknown":
	default:
		return false
	}
	if object.Reason == "" {
		return true
	}
	if strings.ContainsAny(object.Reason, " /:\t\n") {
		return false
	}
	return stableControlErrorCode(object.Reason)
}

func wireApplyState(state string) string {
	if state == "activation-unknown" || state == "commit-requested" {
		return "activation_unknown"
	}
	switch state {
	case "active", "prepared", "aborted":
		return state
	default:
		return "rejected"
	}
}

func stateErrorCode(state string) string {
	switch state {
	case "activation-unknown", "commit-requested":
		return "activation_unknown"
	case "prepare-failed":
		return "prepare_failed"
	case "commit-failed":
		return "commit_failed"
	case "aborted":
		return "aborted"
	case "active":
		return ""
	default:
		return "tx_conflict"
	}
}

func stableConfigError(err error) string {
	var text string
	if err != nil {
		text = err.Error()
	}
	switch {
	case len(text) >= len("identity_mismatch") && text[:len("identity_mismatch")] == "identity_mismatch":
		return "identity_mismatch"
	case len(text) >= len("stale_version") && text[:len("stale_version")] == "stale_version":
		return "stale_version"
	case len(text) >= len("unsupported_config") && text[:len("unsupported_config")] == "unsupported_config":
		return "unsupported_config"
	default:
		return "invalid_config"
	}
}

func storeConfigError(err error) string {
	switch {
	case errors.Is(err, store.ErrConfigRequestConflict), errors.Is(err, ErrConfigConflict):
		return "tx_conflict"
	case errors.Is(err, store.ErrConfigVersionStale), errors.Is(err, ErrStaleRevision):
		return "stale_version"
	case errors.Is(err, store.ErrConfigGenerationStale):
		return "stale_generation"
	case errors.Is(err, store.ErrConfigUnresolved), errors.Is(err, ErrActivationUnknown):
		return "activation_unknown"
	default:
		return "prepare_failed"
	}
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := range left {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}

func validateConfigEnvelope(update *controlv1.RuntimeConfigUpdate) error {
	if update == nil || update.GetVersion() == 0 || update.GetFencingGeneration() == 0 {
		return fmt.Errorf("invalid_config: config update fields are incomplete")
	}
	return nil
}
