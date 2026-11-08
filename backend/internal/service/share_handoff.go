package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
)

// QueueHandoff commits one immutable CAS command only after every predecessor
// signer has durably closed renewal. All capacity stays reserved while delivery
// or activation is unknown. A prepared share is never execution proof.
func (s ShareService) QueueHandoff(ctx context.Context, scope TenantScope, id, request string) error {
	lookup := generated.New(s.Pool)
	target, e := lookup.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: id})
	if e != nil {
		return e
	}
	generation, e := lookup.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: target.NodeID, Kind: target.Kind, Generation: target.Generation})
	if e != nil {
		return e
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.queue_cas", ResourceID: id, RequestID: request, Reason: "close all predecessor issuers before durable CAS task and outbox"}, func(q *generated.Queries) error {
		if e := lockShareIssuersTx(ctx, q, generation); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: id})
		if e != nil {
			return e
		}
		if len(h.Command) != 0 {
			return nil
		}
		chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: h.NodeID, Kind: h.Kind})
		if e != nil {
			return e
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: h.NodeID, Kind: h.Kind, Generation: h.Generation})
		if e != nil {
			return e
		}
		if chain.PendingGeneration != g.Generation || (chain.ActiveGeneration != g.Predecessor && !(chain.ActiveGeneration == 0 && chain.RetiredGeneration == g.Predecessor)) {
			return ErrShareConflict
		}
		_, now, e := s.bounds()
		if e != nil {
			return e
		}
		if !now.Before(g.OwnershipUntil.Time) || !now.Before(g.PolicyUntil.Time) || !now.Before(g.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		// Current rights are checked again immediately before making delivery possible.
		if _, _, e = shareEligibility(ctx, q, scope, PrepareShareInput{NodeID: g.NodeID, BootID: g.BootID, Kind: g.Kind, SnapshotID: g.BillingSnapshotID, PathGeneration: g.PathGeneration, PolicyUntil: g.PolicyUntil.Time}, now); e != nil {
			return e
		}
		closure := ""
		if g.Predecessor > 0 {
			proofs, e := q.ShareClosureEvidence(ctx, generated.ShareClosureEvidenceParams{TenantID: scope.TenantID(), NodeID: g.NodeID, Kind: g.Kind, Generation: g.Predecessor})
			if e != nil {
				return e
			}
			if len(proofs) == 0 {
				return ErrShareUnknown
			}
			prior, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: g.NodeID, Kind: g.Kind, Generation: g.Predecessor})
			if e != nil {
				return e
			}
			closureProofs := [][]byte{}
			for _, p := range proofs {
				proof, e := s.effectiveShareClosureTx(ctx, q, prior, h.ID, p)
				if e != nil {
					return e
				}
				closureProofs = append(closureProofs, proof)
			}
			raw, e := json.Marshal(closureProofs)
			if e != nil {
				return e
			}
			closure = contracts.SharePayloadDigest(raw)
		}
		c := contracts.ShareHandoff{TenantID: scope.TenantID(), ID: g.ID, NodeID: g.NodeID, BootID: g.BootID, Kind: g.Kind, SignerID: g.SignerID, SnapshotID: g.BillingSnapshotID, RuleID: g.RuleID, RuleRevision: g.RuleRevision, Generation: g.Generation, Predecessor: g.Predecessor, SignerEpoch: g.SignerEpoch, PathGeneration: g.PathGeneration, FencingGeneration: g.FencingGeneration, Amount: g.Amount, BurstBytes: g.BurstBytes, ClosureDigest: closure, IssuedAt: now, OwnershipUntil: g.OwnershipUntil.Time.UTC(), PolicyUntil: g.PolicyUntil.Time.UTC(), AutonomyUntil: g.AutonomyUntil.Time.UTC()}
		if e = c.Validate(); e != nil {
			return e
		}
		raw, e := json.Marshal(c)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		n, e := q.BindShareCommand(ctx, generated.BindShareCommandParams{TenantID: scope.TenantID(), ID: id, Command: raw, CommandDigest: sum[:]})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrShareConflict
		}
		return queueShareDelivery(ctx, q, scope, h, "cas")
	})
}
func queueShareDelivery(ctx context.Context, q *generated.Queries, scope TenantScope, h generated.ShareHandoff, phase string) error {
	pending, e := q.PendingShareDelivery(ctx, generated.PendingShareDeliveryParams{TenantID: scope.TenantID(), HandoffID: h.ID})
	if e != nil {
		return e
	}
	if pending {
		return nil
	}
	op, e := newDomainTask(ctx, q, scope, h.ID, "share."+phase, "share_handoff", h.Generation)
	if e != nil {
		return e
	}
	return q.BindShareDelivery(ctx, generated.BindShareDeliveryParams{TenantID: scope.TenantID(), TaskID: op.ID, HandoffID: h.ID, Phase: phase})
}

type ShareDelivery struct {
	Binding generated.GetShareDeliveryRow
	Token   string
}

// LeaseShareDelivery marks activation unknown before any possible send. A
// process crash before transport therefore creates a query, never an unreserved
// retry. Lease, uncertainty, task/outbox and audit share the same transaction.
func (s ShareService) LeaseShareDelivery(ctx context.Context, scope TenantScope, task, request string) (ShareDelivery, error) {
	var out ShareDelivery
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.deliver", ResourceID: task, RequestID: request, Reason: "persist unknown activation before control network send"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		b, e := q.GetShareDelivery(ctx, generated.GetShareDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
		if e != nil {
			return e
		}
		token := uuid.NewString()
		if _, e = q.LeaseShareDelivery(ctx, generated.LeaseShareDeliveryParams{TenantID: scope.TenantID(), ID: task, LeaseToken: pgtype.Text{String: token, Valid: true}}); e != nil {
			return e
		}
		h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: b.HandoffID})
		if e != nil {
			return e
		}
		if h.State == "enforced" {
			return ErrShareConflict
		}
		if b.Phase == "cas" {
			_, now, e := s.bounds()
			if e != nil {
				return e
			}
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: b.NodeID, Kind: b.Kind, Generation: b.Generation})
			if e != nil {
				return e
			}
			if !now.Before(g.OwnershipUntil.Time) || !now.Before(g.PolicyUntil.Time) || !now.Before(g.AutonomyUntil.Time) {
				return ErrLeaseBoundary
			}
			if _, _, e = shareEligibility(ctx, q, scope, PrepareShareInput{NodeID: g.NodeID, BootID: g.BootID, Kind: g.Kind, SnapshotID: g.BillingSnapshotID, PathGeneration: g.PathGeneration, PolicyUntil: g.PolicyUntil.Time}, now); e != nil {
				return e
			}
		}

		if e = q.ShareExecutionUnknown(ctx, generated.ShareExecutionUnknownParams{TenantID: scope.TenantID(), ID: h.ID}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: "running"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: h.OperationID, State: "unknown"}); e != nil {
			return e
		}
		out = ShareDelivery{b, token}
		return nil
	})
	if e != nil {
		return ShareDelivery{}, e
	}
	return out, nil
}

// ShareDeliveryUnknown is an internal worker transition, never release proof.
// The exact task lease prevents a stale worker from finishing another attempt.
func (s ShareService) ShareDeliveryUnknown(ctx context.Context, scope TenantScope, task, token, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.delivery_unknown", ResourceID: task, RequestID: request, Reason: "retain max occupation and query persisted Node floor and enforced capacity"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		b, e := q.GetShareDelivery(ctx, generated.GetShareDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
		if e != nil {
			return e
		}
		t, e := q.ShareTaskByLease(ctx, generated.ShareTaskByLeaseParams{TenantID: scope.TenantID(), ID: task, LeaseToken: pgtype.Text{String: token, Valid: true}})
		if e != nil {
			return e
		}
		if t.State == "succeeded" {
			return nil
		}
		if t.State != "running" && t.State != "unknown" {
			return ErrShareConflict
		}
		if e = q.FinishShareTask(ctx, generated.FinishShareTaskParams{TenantID: scope.TenantID(), ID: task, State: "unknown"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: "unknown"}); e != nil {
			return e
		}
		h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: b.HandoffID})
		if e != nil {
			return e
		}
		if h.State == "enforced" {
			return nil
		}
		if e = q.ShareExecutionUnknown(ctx, generated.ShareExecutionUnknownParams{TenantID: scope.TenantID(), ID: h.ID}); e != nil {
			return e
		}
		return queueShareDelivery(ctx, q, scope, h, "query")
	})
}

// RecordShareReceipt requires a current Node mTLS identity in addition to tenant
// scope. Receipt content binds boot, command digest, share and fencing floors.
// A query reporting missing/unknown, a timeout or lease expiry never releases.
func (s ShareService) RecordShareReceipt(ctx context.Context, scope TenantScope, task, token string, r contracts.ShareObservation, request string) error {
	if e := r.Validate(); e != nil {
		return ErrStaleReceipt
	}
	fingerprint, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.receipt", ResourceID: r.HandoffID, RequestID: request, Reason: "authenticated durable Node CAS/query result; release only proven difference after drain"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		b, e := q.GetShareDelivery(ctx, generated.GetShareDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
		if e != nil {
			return e
		}
		if b.HandoffID != r.HandoffID || b.NodeID != r.NodeID || b.BootID != r.BootID || b.Kind != r.Kind || b.Generation != r.Generation || hex.EncodeToString(b.CommandDigest) != r.CommandDigest || len(r.ReceiptID) != 32 || r.OpenConnections < 0 {
			return ErrStaleReceipt
		}
		node, e := q.GetNodeIncarnation(ctx, r.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != r.BootID {
			return ErrStaleReceipt
		}
		old, e := q.GetShareReceipt(ctx, generated.GetShareReceiptParams{TenantID: scope.TenantID(), ReceiptID: r.ReceiptID})
		if e == nil {
			if !bytes.Equal(old.ReceiptDigest, sum[:]) || !bytes.Equal(old.PeerFingerprint, fingerprint) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		t, e := q.ShareTaskByLease(ctx, generated.ShareTaskByLeaseParams{TenantID: scope.TenantID(), ID: task, LeaseToken: pgtype.Text{String: token, Valid: true}})
		if e != nil {
			return e
		}
		if t.State != "running" && t.State != "unknown" {
			return ErrShareConflict
		}
		h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: scope.TenantID(), ID: b.HandoffID})
		if e != nil {
			return e
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: r.NodeID, Kind: r.Kind, Generation: r.Generation})
		if e != nil {
			return e
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: r.NodeID, BootID: r.BootID})
		if e != nil {
			return e
		}
		if fence.FencingGeneration > g.FencingGeneration || fence.RenewalClosed {
			return ErrStaleReceipt
		}
		if r.State != "enforced" && r.State != "draining" && r.State != "unknown" {
			return ErrStaleReceipt
		}
		enforced := r.State == "enforced" && r.ActiveGeneration == g.Generation && r.MinimumGeneration == g.Generation && r.FencingGeneration == g.FencingGeneration
		if r.State == "enforced" && !enforced {
			return ErrStaleReceipt
		}
		if r.State == "draining" && (r.Kind != "connections" || r.ActiveGeneration != g.Generation || r.MinimumGeneration != g.Generation || r.FencingGeneration != g.FencingGeneration) {
			return ErrStaleReceipt
		}
		if r.Kind == "connections" && r.OpenConnections > g.Amount {
			enforced = false
		}
		if e = q.AppendShareReceipt(ctx, generated.AppendShareReceiptParams{TenantID: scope.TenantID(), HandoffID: h.ID, ReceiptID: r.ReceiptID, CommandDigest: b.CommandDigest, ReceiptDigest: sum[:], PeerFingerprint: fingerprint, Payload: raw}); e != nil {
			return e
		}
		if e = q.FinishShareTask(ctx, generated.FinishShareTaskParams{TenantID: scope.TenantID(), ID: task, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: task}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: "succeeded"}); e != nil {
			return e
		}
		if enforced && h.State != "enforced" {
			n, e := q.ConfirmShareChain(ctx, generated.ConfirmShareChainParams{TenantID: scope.TenantID(), NodeID: r.NodeID, Kind: r.Kind, ActiveGeneration: g.Generation, HeldAmount: g.Amount, ActiveGeneration_2: g.Predecessor})
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrShareConflict
			}
			if e = q.ConfirmShareHandoff(ctx, generated.ConfirmShareHandoffParams{TenantID: scope.TenantID(), ID: h.ID, ReceiptDigest: sum[:]}); e != nil {
				return e
			}
			return q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: h.OperationID, State: "succeeded"})
		}
		if h.State == "enforced" {
			return nil
		}
		if r.State == "draining" {
			n, e := q.ActivateDrainingShare(ctx, generated.ActivateDrainingShareParams{TenantID: scope.TenantID(), NodeID: r.NodeID, Kind: r.Kind, ActiveGeneration: g.Generation, ActiveGeneration_2: g.Predecessor})
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrShareConflict
			}
		}
		if e = q.ShareExecutionUnknown(ctx, generated.ShareExecutionUnknownParams{TenantID: scope.TenantID(), ID: h.ID}); e != nil {
			return e
		}
		return queueShareDelivery(ctx, q, scope, h, "query")
	})
}
