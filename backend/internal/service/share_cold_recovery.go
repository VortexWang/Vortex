package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

// closeColdHubRenewalTx consumes a committed platform proof of all original
// hard deadlines, not a tenant hash or a missing Hub response. It closes only
// the exact predecessor issuer. Node CAS still owns capacity release; financial
// liabilities and all existing child evidence are untouched.
func (s ShareService) closeColdHubRenewalTx(ctx context.Context, q *generated.Queries, scope TenantScope, successor generated.ShareGeneration, authority generated.ShareRenewalAuthority) (bool, error) {
	inc, e := q.LatestHubIncarnation(ctx, authority.SignerID)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if inc.SignerEpoch <= authority.SignerEpoch {
		return false, nil
	}
	cold, e := q.GetHubColdRecovery(ctx, generated.GetHubColdRecoveryParams{HubID: authority.SignerID, ToEpoch: inc.SignerEpoch})
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var proof hubColdRecoveryProof
	hash := sha256.Sum256(cold.Payload)
	if !bytes.Equal(hash[:], cold.PayloadDigest) || json.Unmarshal(cold.Payload, &proof) != nil || proof.Version != 1 || cold.ToBootID != inc.BootID || proof.Request.ID != cold.ID || proof.Request.HubID != authority.SignerID || proof.Request.ToEpoch != inc.SignerEpoch || proof.Request.FromEpoch < authority.SignerEpoch || proof.OldHardDeadline.IsZero() || proof.Earliest.Before(proof.OldHardDeadline) {
		return false, ErrShareUnknown
	}
	lo, _, e := s.bounds()
	if e != nil {
		return false, e
	}
	if lo.Before(proof.OldHardDeadline) {
		return false, ErrShareUnknown
	}
	prior, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: successor.NodeID, Kind: successor.Kind, Generation: successor.Predecessor})
	if e != nil {
		return false, e
	}
	matched := false
	for _, original := range proof.Shares {
		if original.TenantID == scope.TenantID() && original.NodeID == successor.NodeID && original.Kind == successor.Kind && original.Generation == successor.Predecessor && original.SignerID == authority.SignerID && original.SignerEpoch == authority.SignerEpoch && jsonEqual(original, prior) {
			matched = true
			break
		}
	}
	if !matched {
		return false, ErrShareUnknown
	}
	in := RenewalClosure{NodeID: successor.NodeID, BootID: successor.BootID, Kind: successor.Kind, SignerID: authority.SignerID, Generation: successor.Predecessor, SuccessorGeneration: successor.Generation, SignerEpoch: authority.SignerEpoch}
	raw, e := json.Marshal(struct {
		Version        int            `json:"version"`
		RecoveryID     string         `json:"recovery_id"`
		RecoverySHA256 string         `json:"recovery_sha256"`
		Closure        RenewalClosure `json:"closure"`
	}{1, cold.ID, hex.EncodeToString(hash[:]), in})
	if e != nil {
		return false, e
	}
	digest := sha256.Sum256(raw)
	if e = s.closeOldRenewalTx(ctx, q, scope, in, digest[:]); e != nil {
		return false, e
	}
	// Retire unacknowledged live-closure work without manufacturing a Hub
	// receipt or rewriting any already committed page acknowledgement.
	commands, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: scope.TenantID(), HandoffID: successor.ID, SignerID: authority.SignerID})
	if e != nil {
		return false, e
	}
	for _, c := range commands {
		_, e = q.GetShareClosureReceipt(ctx, generated.GetShareClosureReceiptParams{TenantID: scope.TenantID(), CommandID: c.ID})
		if e == nil {
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return false, e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: c.OperationID, State: "failed"}); e != nil {
			return false, e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: c.OperationID, State: "failed"}); e != nil {
			return false, e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: c.OperationID}); e != nil {
			return false, e
		}
	}
	return true, nil
}
