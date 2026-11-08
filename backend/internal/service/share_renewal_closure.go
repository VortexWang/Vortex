package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
)

// PrepareRenewalClosures freezes every old delegation before any send. The
// same tenant lock protects all funding and renewal, so no unlisted old right
// can appear after these immutable barrier records have committed.
func (s ShareService) PrepareRenewalClosures(ctx context.Context, scope TenantScope, handoff, backend, request string) error {
	if !validID(backend) {
		return ErrShareConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.prepare_renewal_closure", ResourceID: handoff, RequestID: request, Reason: "freeze complete predecessor delegation pages before authenticated FULL closure; retain occupation"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: handoff})
		if e != nil {
			return e
		}
		if len(h.Command) != 0 || h.State != "prepared" {
			return nil
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: h.NodeID, Kind: h.Kind, Generation: h.Generation})
		if e != nil {
			return e
		}
		chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: h.NodeID, Kind: h.Kind})
		if e != nil {
			return e
		}
		if g.Predecessor < 1 || chain.BootID != g.BootID || chain.ActiveGeneration != g.Predecessor || chain.PendingGeneration != g.Generation {
			return ErrShareConflict
		}
		node, e := q.GetNodeIncarnation(ctx, g.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != g.BootID {
			return ErrShareUnknown
		}
		authorities, e := q.ShareClosureEvidence(ctx, generated.ShareClosureEvidenceParams{TenantID: scope.TenantID(), NodeID: g.NodeID, Kind: g.Kind, Generation: g.Predecessor})
		if e != nil {
			return e
		}
		if len(authorities) == 0 {
			return ErrShareUnknown
		}
		for _, a := range authorities {
			if a.ClosedAt.Valid {
				continue
			}
			existing, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: scope.TenantID(), HandoffID: h.ID, SignerID: a.SignerID})
			if e != nil {
				return e
			}
			if len(existing) != 0 {
				continue
			}
			incarnation, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: a.SignerID, SignerEpoch: a.SignerEpoch})
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrShareUnknown
			}
			if e != nil {
				return e
			}
			unbound, e := q.UnboundShareClosureDelegations(ctx, generated.UnboundShareClosureDelegationsParams{TenantID: scope.TenantID(), NodeID: g.NodeID, BootID: g.BootID, HubID: a.SignerID, HubBootID: incarnation.BootID})
			if e != nil {
				return e
			}
			if unbound != 0 {
				return ErrShareUnknown
			}
			ids, e := q.ShareClosureDelegations(ctx, generated.ShareClosureDelegationsParams{TenantID: scope.TenantID(), NodeID: g.NodeID, BootID: g.BootID, HubID: a.SignerID, HubBootID: incarnation.BootID, Kind: g.Kind, Generation: g.Predecessor, SignerEpoch: a.SignerEpoch})
			if e != nil {
				return e
			}
			parts := max(1, (len(ids)+63)/64)
			if parts > math.MaxInt32 {
				return ErrShareConflict
			}
			for part := 0; part < parts; part++ {
				page := append([]string{}, ids[min(part*64, len(ids)):min((part+1)*64, len(ids))]...)
				c := contracts.ShareRenewalClosure{Version: 1, ID: uuid.NewString(), BackendID: backend, TenantID: scope.TenantID(), HandoffID: h.ID, HubID: a.SignerID, HubBootID: incarnation.BootID, NodeID: g.NodeID, BootID: g.BootID, Kind: g.Kind, Generation: g.Predecessor, SuccessorGeneration: g.Generation, SignerEpoch: a.SignerEpoch, Part: int32(part + 1), Parts: int32(parts), DelegationIDs: page}
				if e = c.Validate(); e != nil {
					return e
				}
				raw, e := json.Marshal(c)
				if e != nil {
					return e
				}
				sum := sha256.Sum256(raw)
				op, e := newDomainTask(ctx, q, scope, c.ID, "share.renewal_closure", "resource_change", g.Generation)
				if e != nil {
					return e
				}
				if e = q.AppendShareClosureCommand(ctx, generated.AppendShareClosureCommandParams{TenantID: scope.TenantID(), ID: c.ID, HandoffID: h.ID, NodeID: g.NodeID, BootID: g.BootID, Kind: g.Kind, Generation: g.Predecessor, SignerID: a.SignerID, SignerEpoch: a.SignerEpoch, HubBootID: incarnation.BootID, Part: c.Part, Parts: c.Parts, OperationID: op.ID, Payload: raw, PayloadDigest: sum[:]}); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

func (s ShareService) PendingRenewalClosures(ctx context.Context, hub, boot string) ([]generated.ShareRenewalClosureCommand, error) {
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hub); e != nil {
		return nil, e
	}
	return generated.New(s.Pool).PendingShareClosures(ctx, generated.PendingShareClosuresParams{SignerID: hub, HubBootID: boot})
}

// RecordRenewalClosureReceipt acknowledges only the exact persisted page. The
// last page closes issuance in the same audit transaction. This is never a CAS
// or child-stop proof and therefore never releases capacity or financial holds.
func (s ShareService) RecordRenewalClosureReceipt(ctx context.Context, hub, boot string, raw []byte, request string) error {
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hub)
	if e != nil {
		return e
	}
	r, e := contracts.DecodeShareRenewalClosureReceipt(raw)
	if e != nil {
		return ErrShareConflict
	}
	command, e := generated.New(s.Pool).GetShareClosureCommand(ctx, r.ID)
	if e != nil {
		return e
	}
	if command.SignerID != hub || command.HubBootID != boot || hex.EncodeToString(command.PayloadDigest) != r.CommandDigest {
		return ErrShareConflict
	}
	c, e := contracts.DecodeShareRenewalClosure(command.Payload)
	if e != nil {
		return e
	}
	scope, e := hubScope(hub, command.TenantID)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.renewal_closed_receipt", ResourceID: c.ID, RequestID: request, Reason: "authenticated FULL old-issuer page; close only when every immutable page is confirmed"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetShareClosureReceipt(ctx, generated.GetShareClosureReceiptParams{TenantID: scope.TenantID(), CommandID: c.ID})
		if e == nil {
			if !bytes.Equal(old.Payload, raw) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = q.AppendShareClosureReceipt(ctx, generated.AppendShareClosureReceiptParams{TenantID: scope.TenantID(), CommandID: c.ID, Payload: raw, PayloadDigest: sum[:], PeerFingerprint: fingerprint}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: command.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: command.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: command.OperationID}); e != nil {
			return e
		}
		remaining, e := q.RemainingShareClosures(ctx, generated.RemainingShareClosuresParams{TenantID: scope.TenantID(), HandoffID: c.HandoffID, SignerID: hub})
		if e != nil {
			return e
		}
		if remaining != 0 {
			return nil
		}
		in := RenewalClosure{NodeID: c.NodeID, BootID: c.BootID, Kind: c.Kind, SignerID: c.HubID, Generation: c.Generation, SuccessorGeneration: c.SuccessorGeneration, SignerEpoch: c.SignerEpoch}
		closure, e := json.Marshal(in)
		if e != nil {
			return e
		}
		proof := sha256.Sum256(append(closure, fingerprint...))
		return s.closeOldRenewalTx(ctx, q, scope, in, proof[:])
	})
}
