package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// HubRecoveryPreflight locates only an existing explicit approval for this
// authenticated actual boot. An available funded session is advisory: the
// separate funded stream must still perform BindHubSession and all gates.
// No heartbeat, incarnation, local ACK or financial state changes here.
func (s ShareService) HubRecoveryPreflight(ctx context.Context, hubID, boot, backend, request string) (generated.HubSameStoreRecovery, bool, error) {
	var out generated.HubSameStoreRecovery
	available := false
	if !validID(boot) || !validID(backend) {
		return out, false, ErrShareConflict
	}
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hubID); e != nil {
		return out, false, e
	}
	boundary, e := generated.New(s.Pool).HubSignerBoundary(ctx, hubID)
	if e != nil {
		return out, false, e
	}
	scope, e := hubScope(hubID, boundary.TenantID)
	if e != nil {
		return out, false, e
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "hub.recovery_preflight", ResourceID: hubID, RequestID: request, Reason: "boot-bound explicit approval discovery; no heartbeat, bind, ACK or online admission"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hubID); e != nil {
			return e
		}
		current, e := q.HubSignerBoundary(ctx, hubID)
		if e != nil {
			return e
		}
		if current.TenantID != boundary.TenantID {
			return ErrShareConflict
		}
		prior, e := q.LatestHubIncarnation(ctx, hubID)
		if errors.Is(e, pgx.ErrNoRows) {
			available = true
			return nil
		}
		if e != nil {
			return e
		}
		if prior.BootID == boot && prior.SignerEpoch == current.SignerEpoch {
			available = true
			return nil
		}
		row, e := q.HubSameStoreRecoveryByTargetBoot(ctx, generated.HubSameStoreRecoveryByTargetBootParams{HubID: hubID, ToBootID: boot})
		if e != nil {
			return e
		}
		_, c, e := sameStoreCommand(ctx, q, row.ID, hubID, boot)
		if e != nil {
			return e
		}
		if c.BackendID != backend {
			return ErrShareConflict
		}
		if e = s.pendingSameStore(ctx, q, c); e != nil {
			return e
		}
		out = row
		return nil
	})
	return out, available, e
}
