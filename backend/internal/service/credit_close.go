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
	"google.golang.org/protobuf/proto"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// PrepareCreditClose only closes a child from a complete, authenticated issuer
// seal. Issuer expiry or a missing installation ACK alone cannot create work.
func (s ShareService) PrepareCreditClose(ctx context.Context, scope TenantScope, grant, backend, request string) (generated.NodeCreditCloseCommand, error) {
	var out generated.NodeCreditCloseCommand
	if !validID(backend) {
		return out, ErrScope
	}
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.prepare_credit_close", ResourceID: grant, RequestID: request, Reason: "complete sealed issuer inventory binds an immutable original-child stop command; funds remain held"}, func(q *generated.Queries) error {
		if _, err := q.LockTenant(ctx, scope.TenantID()); err != nil {
			return err
		}
		old, err := q.GetCreditCloseCommand(ctx, generated.GetCreditCloseCommandParams{TenantID: scope.TenantID(), GrantID: grant})
		if err == nil {
			if old.BackendID != backend {
				return ErrShareConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		child, err := q.HubCreditGrant(ctx, generated.HubCreditGrantParams{TenantID: scope.TenantID(), GrantID: grant})
		if err != nil {
			return err
		}
		if _, err = q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: scope.TenantID(), AllocationID: child.AllocationID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrShareUnknown
		} else if err != nil {
			return err
		}
		a, err := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: child.AllocationID})
		if err != nil {
			return err
		}
		allocation, err := hub.DecodeAllocation(a.Payload)
		if err != nil {
			return err
		}
		issue, err := q.GetHubIssue(ctx, generated.GetHubIssueParams{TenantID: scope.TenantID(), IssueID: child.IssueID})
		if err != nil {
			return err
		}
		c := contracts.CreditCloseCommand{Version: 1, ID: uuid.NewString(), BackendID: backend, HubID: a.HubID, NodeID: a.NodeID, BootID: a.BootID, GrantID: grant, Generation: allocation.PathGeneration, Original: issue.Payload}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = contracts.DecodeCreditCloseCommand(raw); err != nil {
			return err
		}
		if _, err = agent.InspectIssuedCreditGrant(c.Original, c.NodeID, c.BootID, c.HubID, c.GrantID, c.Generation); err != nil {
			return err
		}
		op, err := newDomainTask(ctx, q, scope, a.NodeID, "node.close_credit", "resource_change", int64(c.Generation))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if err = q.AppendCreditCloseCommand(ctx, generated.AppendCreditCloseCommandParams{TenantID: scope.TenantID(), GrantID: grant, AllocationID: a.AllocationID, CommandID: c.ID, NodeID: c.NodeID, BootID: c.BootID, BackendID: backend, OperationID: op.ID, Payload: raw, PayloadDigest: sum[:]}); err != nil {
			return err
		}
		out, err = q.GetCreditCloseCommand(ctx, generated.GetCreditCloseCommandParams{TenantID: scope.TenantID(), GrantID: grant})
		return err
	})
	if err != nil {
		return generated.NodeCreditCloseCommand{}, err
	}
	return out, nil
}

// RecordCreditClose authenticates the actual Node stream, matches immutable
// command/original issue scope and independently rebuilds the complete known
// wire inventory. This records child evidence only; parent release is separate.
func (s ShareService) RecordCreditClose(ctx context.Context, scope TenantScope, raw []byte, request string) error {
	r, err := contracts.DecodeCreditCloseReceipt(raw)
	if err != nil {
		return ErrShareConflict
	}
	fingerprint, err := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID)
	if err != nil {
		return err
	}
	if r.State != "final" {
		return ErrShareUnknown
	}
	var final store.CreditGrantFinal
	if json.Unmarshal(r.Final, &final) != nil {
		return ErrShareConflict
	}
	canonical, err := json.Marshal(final)
	if err != nil || !bytes.Equal(canonical, r.Final) || final.KnownUsage.Validate() != nil {
		return ErrShareUnknown
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.credit_closed", ResourceID: r.GrantID, RequestID: request, Reason: "authenticated FULL child final and independently reconstructed committed wire inventory; no parent release"}, func(q *generated.Queries) error {
		if _, err := q.LockTenant(ctx, scope.TenantID()); err != nil {
			return err
		}
		p, err := q.GetCreditCloseCommand(ctx, generated.GetCreditCloseCommandParams{TenantID: scope.TenantID(), GrantID: r.GrantID})
		if err != nil {
			return err
		}
		if r.CommandID != p.CommandID || r.CommandSHA256 != hex.EncodeToString(p.PayloadDigest) || r.NodeID != p.NodeID || r.BootID != p.BootID || r.BackendID != p.BackendID {
			return ErrStaleReceipt
		}
		command, err := contracts.DecodeCreditCloseCommand(p.Payload)
		if err != nil {
			return err
		}
		expected, err := agent.InspectIssuedCreditGrant(command.Original, command.NodeID, command.BootID, command.HubID, command.GrantID, command.Generation)
		if err != nil {
			return err
		}
		actual := final.Grant
		actual.ShareRevision = 0 // local reference-head observation, never issuer scope
		if final.NodeID != p.NodeID || actual != expected || final.Used > expected.AmountBytes || final.Unused != expected.AmountBytes-final.Used || final.KnownUsage.Count > final.BatchCount || final.NeverInstalled && (final.Used != 0 || final.BatchCount != 0 || final.KnownUsage.Count != 0) {
			return ErrShareConflict
		}
		old, err := q.GetCreditCloseReceipt(ctx, generated.GetCreditCloseReceiptParams{TenantID: scope.TenantID(), GrantID: r.GrantID})
		if err == nil {
			if !bytes.Equal(old.Payload, raw) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Match wire ingestion's lock order before reading usage projections.
		if err = q.LockUsageTuple(ctx, p.NodeID+"/"+p.BootID); err != nil {
			return err
		}
		if err = q.LockUsageTuple(ctx, "funded-usage/"+scope.TenantID()); err != nil {
			return err
		}
		child, err := q.HubCreditGrant(ctx, generated.HubCreditGrantParams{TenantID: scope.TenantID(), GrantID: r.GrantID})
		if err != nil {
			return err
		}
		parent, err := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: child.ReservationID})
		if err != nil {
			return err
		}
		if _, err = q.LockSourceProjection(ctx, generated.LockSourceProjectionParams{TenantID: scope.TenantID(), SourceID: parent.SourceID}); err != nil {
			return err
		}
		if _, err = q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: scope.TenantID(), GrantID: parent.ID}); err == nil {
			return ErrShareUnknown
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		disputed, err := q.ReservationDisputed(ctx, generated.ReservationDisputedParams{TenantID: scope.TenantID(), GrantID: parent.ID})
		if err != nil {
			return err
		}
		if disputed {
			return ErrShareUnknown
		}
		if err = verifyChildKnownUsage(ctx, q, scope.TenantID(), command, final); err != nil {
			return err
		}
		if err = q.AppendCreditCloseReceipt(ctx, generated.AppendCreditCloseReceiptParams{TenantID: scope.TenantID(), GrantID: r.GrantID, Payload: raw, PayloadDigest: sum[:], PeerFingerprint: fingerprint}); err != nil {
			return err
		}
		if err = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.OperationID, State: "succeeded"}); err != nil {
			return err
		}
		if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.OperationID, State: "succeeded"}); err != nil {
			return err
		}
		return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: p.OperationID})
	})
}

func verifyChildKnownUsage(ctx context.Context, q *generated.Queries, tenant string, c contracts.CreditCloseCommand, final store.CreditGrantFinal) error {
	rows, err := q.ChildKnownUsage(ctx, generated.ChildKnownUsageParams{TenantID: tenant, NodeID: c.NodeID, BootID: c.BootID, GrantID: c.GrantID})
	if err != nil {
		return err
	}
	var batches []contracts.KnownUsageBatch
	var used uint64
	for _, row := range rows {
		sum := sha256.Sum256(row.Payload)
		if !bytes.Equal(sum[:], row.PayloadSha256) || row.BillingSnapshotID != final.Grant.BillingSnapshotID {
			return ErrShareConflict
		}
		var batch controlv1.UsageBatch
		if proto.Unmarshal(row.Payload, &batch) != nil || len(batch.ProtoReflect().GetUnknown()) != 0 || batch.BatchId != row.BatchID || batch.BillingSnapshotId != row.BillingSnapshotID {
			return ErrShareConflict
		}
		wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(&batch)
		if err != nil || !bytes.Equal(wire, row.Payload) {
			return ErrShareConflict
		}
		found := false
		for _, d := range batch.Deltas {
			if d == nil || d.GrantId != c.GrantID {
				continue
			}
			if len(d.ProtoReflect().GetUnknown()) != 0 || d.RuleId != final.Grant.RuleID || d.Role != "source" && d.Role != "execution" || d.Direction != "ingress" && d.Direction != "egress" || d.Bytes > math.MaxInt64 || used > final.Used || d.Bytes > final.Used-used {
				return ErrShareConflict
			}
			found = true
			used += d.Bytes
		}
		if !found {
			return ErrShareConflict
		}
		batches = append(batches, contracts.KnownUsageBatch{BatchID: row.BatchID, SHA256: hex.EncodeToString(sum[:])})
	}
	inventory, err := contracts.BuildKnownUsageInventory(batches)
	if err != nil {
		return err
	}
	previous, err := q.HubCreditUsage(ctx, generated.HubCreditUsageParams{TenantID: tenant, GrantID: c.GrantID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if inventory != final.KnownUsage || used != final.Used || previous < 0 || uint64(previous) != used {
		return ErrShareUnknown
	}
	return nil
}
