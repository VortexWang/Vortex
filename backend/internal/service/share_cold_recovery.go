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
	prior, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: successor.NodeID, Kind: successor.Kind, Generation: successor.Predecessor})
	if e != nil {
		return false, e
	}
	digest, found, e := s.coldShareClosureProofTx(ctx, q, prior, successor.Generation, authority)
	if e != nil || !found {
		return false, e
	}
	// An S/R admission already closed the original row. Preserve that exact
	// historical proof; the independent cold proof closes its later issuers.
	if !authority.ClosedAt.Valid {
		in := RenewalClosure{NodeID: successor.NodeID, BootID: successor.BootID, Kind: successor.Kind, SignerID: authority.SignerID, Generation: successor.Predecessor, SuccessorGeneration: successor.Generation, SignerEpoch: authority.SignerEpoch}
		if e = s.closeOldRenewalTx(ctx, q, scope, in, digest); e != nil {
			return false, e
		}
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

// coldShareClosureProofTx validates a committed platform inventory independently
// of the live S/R chain. It grants no renewal or funding authority. Every issuer
// covered by this proof has passed its original hard deadline; Node CAS and
// financial closure remain separate proofs. Caller holds signer then tenant locks.
func (s ShareService) coldShareClosureProofTx(ctx context.Context, q *generated.Queries, prior generated.ShareGeneration, successor int64, authority generated.ShareRenewalAuthority) ([]byte, bool, error) {
	inc, e := q.LatestHubIncarnation(ctx, authority.SignerID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	if inc.SignerEpoch <= authority.SignerEpoch {
		return nil, false, nil
	}
	cold, e := q.GetHubColdRecovery(ctx, generated.GetHubColdRecoveryParams{HubID: authority.SignerID, ToEpoch: inc.SignerEpoch})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	var proof hubColdRecoveryProof
	hash := sha256.Sum256(cold.Payload)
	if !bytes.Equal(hash[:], cold.PayloadDigest) || json.Unmarshal(cold.Payload, &proof) != nil {
		return nil, false, ErrShareUnknown
	}
	canonical, e := json.Marshal(proof)
	if e != nil || !bytes.Equal(canonical, cold.Payload) || proof.Version != 1 ||
		cold.HubID != authority.SignerID || cold.ToEpoch != inc.SignerEpoch || cold.ToBootID != inc.BootID ||
		proof.Request.ID != cold.ID || proof.Request.HubID != cold.HubID ||
		proof.Request.FromEpoch != cold.FromEpoch || proof.Request.ToEpoch != cold.ToEpoch ||
		proof.Request.FromBootID != cold.FromBootID || proof.Request.ToBootID != cold.ToBootID ||
		cold.FromEpoch < authority.SignerEpoch || cold.FromEpoch >= cold.ToEpoch || cold.FromBootID == cold.ToBootID ||
		proof.OldHardDeadline.IsZero() || proof.Earliest.Before(proof.OldHardDeadline) || proof.Latest.Before(proof.Earliest) ||
		prior.SignerID != authority.SignerID || prior.SignerEpoch != authority.SignerEpoch ||
		prior.TenantID != authority.TenantID || prior.NodeID != authority.NodeID || prior.Kind != authority.Kind || prior.Generation != authority.Generation || successor != prior.Generation+1 ||
		proof.OldHardDeadline.Before(prior.OwnershipUntil.Time) || proof.OldHardDeadline.Before(prior.PolicyUntil.Time) || proof.OldHardDeadline.Before(prior.AutonomyUntil.Time) {
		return nil, false, ErrShareUnknown
	}
	from, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: cold.HubID, SignerEpoch: cold.FromEpoch})
	if e != nil || from.BootID != cold.FromBootID {
		return nil, false, ErrShareUnknown
	}
	lo, _, e := s.bounds()
	if e != nil {
		return nil, false, e
	}
	if lo.Before(proof.OldHardDeadline) {
		return nil, false, ErrShareUnknown
	}
	matched := 0
	for _, original := range proof.Shares {
		if original.TenantID == prior.TenantID && original.NodeID == prior.NodeID && original.Kind == prior.Kind && original.Generation == prior.Generation {
			if !jsonEqual(original, prior) {
				return nil, false, ErrShareUnknown
			}
			matched++
		}
	}
	if matched != 1 {
		return nil, false, ErrShareUnknown
	}
	in := RenewalClosure{NodeID: prior.NodeID, BootID: prior.BootID, Kind: prior.Kind, SignerID: authority.SignerID, Generation: prior.Generation, SuccessorGeneration: successor, SignerEpoch: authority.SignerEpoch}
	raw, e := json.Marshal(struct {
		Version        int            `json:"version"`
		RecoveryID     string         `json:"recovery_id"`
		RecoverySHA256 string         `json:"recovery_sha256"`
		Closure        RenewalClosure `json:"closure"`
	}{1, cold.ID, hex.EncodeToString(hash[:]), in})
	if e != nil {
		return nil, false, e
	}
	digest := sha256.Sum256(raw)
	return digest[:], true, nil
}
