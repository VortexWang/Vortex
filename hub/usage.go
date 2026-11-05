package hub

import (
	"context"
	"crypto/sha256"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// UsageBackend is an authenticated backend adapter. It returns the digest only
// after durable, idempotent acceptance. Phase 5 tests supply a protocol fixture;
// the production backend transport belongs to Phase 6.
type UsageBackend interface {
	AcceptUsage(context.Context, store.HubUsage) ([]byte, error)
}

type UsageRelay struct {
	Store   *store.Store
	Clock   func() store.IssuerTime
	Backend UsageBackend
}

func (r *UsageRelay) AcceptKnown(ctx context.Context, node, boot string, b *controlv1.UsageBatch) error {
	if r == nil || r.Store == nil || b == nil || b.GetBatchId() == "" || b.GetBillingSnapshotId() == "" {
		return fmt.Errorf("hub: incomplete known usage")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(b)
	if err != nil {
		return err
	}
	if err := r.Store.PersistUsageLedger(ctx, store.UsageBatch{NodeID: node, BootID: boot, BatchID: b.BatchId, SnapshotID: b.BillingSnapshotId, Payload: payload}); err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	return r.ackBoundary(ctx, store.HubUsage{NodeID: node, BootID: boot, BatchID: b.BatchId, Kind: "known", Payload: payload, Digest: digest[:]})
}

func (r *UsageRelay) AcceptUnknown(ctx context.Context, node string, b *controlv1.UnknownUsageBatch) error {
	if r == nil || r.Store == nil {
		return fmt.Errorf("hub: missing durable usage store")
	}
	payload, err := contracts.EncodeUnknownUsage(b)
	if err != nil {
		return err
	}
	if err := r.Store.PersistUnknownUsage(ctx, node, payload); err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	return r.ackBoundary(ctx, store.HubUsage{NodeID: node, BootID: b.OriginBootId, BatchID: b.BatchId, Kind: "unknown", Payload: payload, Digest: digest[:]})
}

func (r *UsageRelay) ackBoundary(ctx context.Context, b store.HubUsage) error {
	if r.Clock == nil {
		return fmt.Errorf("hub: trusted accounting clock missing")
	}
	now := r.Clock()
	if !now.Trusted || now.Earliest.IsZero() || now.Latest.Before(now.Earliest) {
		return store.ErrAuthorityTimeUncertain
	}
	mode, err := r.Store.CurrentAutonomyStatus(ctx, now.Latest, true)
	if err != nil {
		return err
	}
	if now.Earliest.Before(mode.BackendHeartbeat) {
		return store.ErrAuthorityTimeUncertain
	}
	if mode.Mode != store.AutonomyOnline {
		// Accounting survives authority expiry: retain and acknowledge locally,
		// but grant issuance remains independently stopped at its hard boundary.
		return nil
	}
	return r.forward(ctx, b)
}

func (r *UsageRelay) forward(ctx context.Context, b store.HubUsage) error {
	confirmed, err := r.Store.BackendUsageConfirmed(ctx, b)
	if err != nil || confirmed {
		return err
	}
	if r.Backend == nil {
		return fmt.Errorf("hub: online usage requires backend confirmation")
	}
	digest, err := r.Backend.AcceptUsage(ctx, b)
	if err != nil {
		return err
	}
	return r.Store.ConfirmBackendUsage(ctx, b, digest)
}

// ReplayPending drains durable online/autonomous batches without changing the
// signing epoch, deadline, credit balances, or unknown-liability state. Recovery
// policy must apply revocations before invoking this backend adapter.
func (r *UsageRelay) ReplayPending(ctx context.Context) error {
	if r == nil || r.Store == nil {
		return fmt.Errorf("hub: missing durable usage store")
	}
	batches, err := r.Store.PendingBackendUsage(ctx)
	if err != nil {
		return err
	}
	for _, b := range batches {
		if err := r.forward(ctx, b); err != nil {
			return err
		}
	}
	return nil
}
