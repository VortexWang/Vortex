package agent

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// PersistUsageBatch is the Engine-to-Agent durability boundary. The batch is
// committed locally before it can be sent to a Hub or acknowledged in autonomy.
func (n *Node) PersistUsageBatch(ctx context.Context, batch *controlv1.UsageBatch, unknownUpperBound uint64) error {
	if n == nil || n.Store == nil || batch == nil || batch.GetBatchId() == "" || batch.GetBillingSnapshotId() == "" {
		return fmt.Errorf("agent: usage batch identity and billing snapshot are required")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(batch)
	if err != nil {
		return fmt.Errorf("agent: encode usage batch: %w", err)
	}
	return n.Store.PersistUsageOutbox(ctx, store.UsageBatch{
		NodeID: n.NodeID, BootID: n.BootID, BatchID: batch.GetBatchId(),
		SnapshotID: batch.GetBillingSnapshotId(), Payload: payload,
		UnknownUpper: unknownUpperBound,
	})
}

// AcknowledgeUsageLocally records local completion without deleting the batch;
// PendingUsageOutbox will continue handing it off after control connectivity returns.
func (n *Node) AcknowledgeUsageLocally(ctx context.Context, batchIDs []string) error {
	if n == nil || n.Store == nil {
		return fmt.Errorf("agent: usage store is required")
	}
	return n.Store.MarkUsageLocallyAcknowledged(ctx, n.NodeID, batchIDs)
}
