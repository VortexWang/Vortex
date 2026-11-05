package agent

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	engineaccounting "vortex/engine/accounting"
	runtimeaccounting "vortex/engine/runtime/accounting"
)

// NewCreditBook is the process-internal Agent-to-Engine accounting boundary.
// The Engine receives only the UsageSink interface and never opens SQLite.
func (n *Node) NewCreditBook(config runtimeaccounting.BookConfig) (*runtimeaccounting.CreditBook, error) {
	if n == nil || n.Store == nil {
		return nil, fmt.Errorf("agent: durable usage store is required")
	}
	return runtimeaccounting.NewCreditBookWithUsage(config, n)
}

// ReserveUsage implements the Engine pre-write durability boundary.
func (n *Node) ReserveUsage(ctx context.Context, record engineaccounting.UsageRecord) error {
	if n == nil || n.Store == nil || record.Reserved <= 0 {
		return fmt.Errorf("agent: invalid Engine usage reservation")
	}
	direction, err := usageDirection(record.Direction)
	if err != nil {
		return err
	}
	return n.Store.ReserveUsage(ctx, store.UsageReservation{
		NodeID: n.NodeID, BootID: n.BootID, BatchID: record.BatchID,
		SnapshotID: record.SnapshotID, FlowKey: record.FlowKey, RuleID: record.RuleID,
		Direction: direction, Reserved: uint64(record.Reserved),
	})
}

func (n *Node) recoverUsageReservations(ctx context.Context) error {
	reservations, err := n.Store.UnresolvedUsageReservations(ctx, n.NodeID, n.BootID)
	if err != nil {
		return err
	}
	for _, reservation := range reservations {
		direction := engineaccounting.LeftToRight
		if reservation.Direction == "ingress" {
			direction = engineaccounting.RightToLeft
		}
		record := engineaccounting.UsageRecord{
			BatchID: reservation.BatchID, FlowKey: reservation.FlowKey, RuleID: reservation.RuleID,
			SnapshotID: reservation.SnapshotID, Direction: direction,
			Resolution: engineaccounting.Unknown, Reserved: int64(reservation.Reserved), Used: int64(reservation.Reserved),
		}
		payload, err := marshalUsage(record, reservation.Direction, reservation.Reserved)
		if err != nil {
			return err
		}
		if err := n.Store.SettleUsageReservation(ctx, reservation, "unknown", reservation.Reserved, payload, reservation.Reserved); err != nil {
			return fmt.Errorf("freeze recovered reservation %s: %w", reservation.BatchID, err)
		}
	}
	return nil
}

// SettleUsage commits resolution and outbox payload in one SQLite transaction.
func (n *Node) SettleUsage(ctx context.Context, record engineaccounting.UsageRecord) error {
	if n == nil || n.Store == nil || record.Reserved <= 0 || record.Used < 0 || record.Used > record.Reserved {
		return fmt.Errorf("agent: invalid Engine usage settlement")
	}
	direction, err := usageDirection(record.Direction)
	if err != nil {
		return err
	}
	reservation := store.UsageReservation{
		NodeID: n.NodeID, BootID: n.BootID, BatchID: record.BatchID,
		SnapshotID: record.SnapshotID, FlowKey: record.FlowKey, RuleID: record.RuleID,
		Direction: direction, Reserved: uint64(record.Reserved),
	}
	used, unknownUpper := uint64(record.Used), uint64(0)
	var payload []byte
	switch record.Resolution {
	case engineaccounting.Returned:
		used = 0
	case engineaccounting.Committed:
		payload, err = marshalUsage(record, direction, used)
	case engineaccounting.Unknown:
		used = uint64(record.Reserved)
		unknownUpper = uint64(record.Reserved)
		payload, err = marshalUsage(record, direction, used)
	default:
		return fmt.Errorf("agent: unsupported Engine usage resolution")
	}
	if err != nil {
		return err
	}
	return n.Store.SettleUsageReservation(ctx, reservation, string(record.Resolution), used, payload, unknownUpper)
}

func usageDirection(direction engineaccounting.Direction) (string, error) {
	switch direction {
	case engineaccounting.LeftToRight:
		return "egress", nil
	case engineaccounting.RightToLeft:
		return "ingress", nil
	default:
		return "", fmt.Errorf("agent: invalid Engine usage direction")
	}
}

func marshalUsage(record engineaccounting.UsageRecord, direction string, bytesUsed uint64) ([]byte, error) {
	batch := &controlv1.UsageBatch{
		BatchId: record.BatchID, BillingSnapshotId: record.SnapshotID,
		Deltas: []*controlv1.UsageDelta{{RuleId: record.RuleID, Role: "source", Direction: direction, Bytes: bytesUsed}},
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("agent: encode usage batch: %w", err)
	}
	return payload, nil
}

// PersistUsage implements the Engine's database-free in-process accounting
// interface. The FULL SQLite transaction completes before Engine settles its hold.
func (n *Node) PersistUsage(ctx context.Context, record engineaccounting.UsageRecord) error {
	if n == nil || n.Store == nil || record.BatchID == "" || record.SnapshotID == "" || record.RuleID == "" || record.Reserved <= 0 || record.Used < 0 || record.Used > record.Reserved {
		return fmt.Errorf("agent: incomplete Engine usage record")
	}
	bytesUsed := uint64(record.Used)
	unknownUpper := uint64(0)
	if record.Resolution == engineaccounting.Unknown {
		bytesUsed = uint64(record.Reserved)
		unknownUpper = uint64(record.Reserved)
	} else if record.Resolution != engineaccounting.Committed {
		return fmt.Errorf("agent: unsupported Engine usage resolution")
	}
	direction, err := usageDirection(record.Direction)
	if err != nil {
		return err
	}
	payload, err := marshalUsage(record, direction, bytesUsed)
	if err != nil {
		return err
	}
	var batch controlv1.UsageBatch
	if err := proto.Unmarshal(payload, &batch); err != nil {
		return err
	}
	return n.PersistUsageBatch(ctx, &batch, unknownUpper)
}

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
