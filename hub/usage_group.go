package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// A backend may share its durable commit, while preserving each record's
// independent identity, digest and validation. Others retain sequential calls.
type usageGroupBackend interface {
	AcceptUsageGroup(context.Context, []store.HubUsage) ([][]byte, error)
}

func (r *UsageRelay) AcceptKnownEnvelopes(ctx context.Context, messages []*controlv1.NodeMessage) error {
	if r == nil || r.Store == nil || len(messages) == 0 || len(messages) > 32 {
		return store.ErrUsageBatchConflict
	}
	batches := make([]store.HubUsage, 0, len(messages))
	for _, m := range messages {
		b := m.GetUsageBatch()
		if b == nil || m.NodeId == "" || m.BootId == "" || m.Sequence == 0 {
			return store.ErrUsageOriginMissing
		}
		payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(b)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		batches = append(batches, store.HubUsage{NodeID: m.NodeId, BootID: m.BootId, BatchID: b.BatchId, Kind: "known", Payload: payload, Digest: digest[:], ReportBootID: m.BootId, Sequence: m.Sequence})
	}
	batches, err := r.Store.PersistKnownUsageGroup(ctx, batches, true)
	if err != nil {
		return err
	}
	// The same time/mode boundary governs single and grouped reports.
	return r.ackBoundaryGroup(ctx, batches)
}

func (r *UsageRelay) forwardGroup(ctx context.Context, batches []store.HubUsage) error {
	var pending []store.HubUsage
	for _, b := range batches {
		confirmed, err := r.Store.BackendUsageConfirmed(ctx, b)
		if err != nil {
			return err
		}
		if !confirmed {
			pending = append(pending, b)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if r.Backend == nil {
		return fmt.Errorf("hub: online usage requires backend confirmation")
	}
	var digests [][]byte
	if grouped, ok := r.Backend.(usageGroupBackend); ok {
		var err error
		digests, err = grouped.AcceptUsageGroup(ctx, pending)
		if err != nil {
			return err
		}
	} else {
		for _, b := range pending {
			digest, err := r.Backend.AcceptUsage(ctx, b)
			if err != nil {
				return err
			}
			digests = append(digests, digest)
		}
	}
	if len(digests) != len(pending) {
		return store.ErrUsageBatchConflict
	}
	for i, b := range pending {
		if !bytes.Equal(digests[i], b.Digest) {
			return store.ErrUsageBatchConflict
		}
	}
	return r.Store.ConfirmBackendUsageGroup(ctx, pending)
}
