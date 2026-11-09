package agent

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// One scan advances over durable insert order. New-row hints resume it without
// repeating in-flight rows. Periodic replay and reconnect start from zero.
type usageDeliveryCursor struct {
	knownAfter, unknownAfter int64
	knownDone, unknownDone   bool
	nextUnknown              bool
}

func (d *usageDeliveryCursor) resume() { d.knownDone, d.unknownDone = false, false }
func (d *usageDeliveryCursor) reset()  { *d = usageDeliveryCursor{} }

// Each turn sends at most 32 records, alternating known and typed unknown
// pages. Only a successful send advances its cursor; dispatch never changes
// durable ACK or unknown-liability state. The caller schedules another turn
// only when more is true, a new-row hint arrives, or the replay timer fires.
func (c *ControlClient) sendUsageDeliveryPage(ctx context.Context, stream controlv1.NodeControl_ConnectClient, lastSequence *uint64, cursor *usageDeliveryCursor) (bool, error) {
	for turn := 0; turn < 2; turn++ {
		unknown := cursor.nextUnknown
		cursor.nextUnknown = !unknown
		if unknown {
			if cursor.unknownDone {
				continue
			}
			page, err := c.Node.Store.UnknownUsageDeliveryPage(ctx, c.Node.NodeID, cursor.unknownAfter)
			if err != nil {
				return false, err
			}
			if len(page) == 0 {
				cursor.unknownDone = true
				continue
			}
			first, err := c.Node.Store.NextControlSequenceRange(ctx, uint64(len(page)))
			if err != nil {
				return false, err
			}
			for i, item := range page {
				batch, err := contracts.DecodeUnknownUsage(item.Batch.Payload)
				if err != nil {
					return false, err
				}
				seq := first + uint64(i)
				if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: seq, Body: &controlv1.NodeMessage_UnknownUsageBatch{UnknownUsageBatch: batch}}); err != nil {
					return false, err
				}
				*lastSequence, cursor.unknownAfter = seq, item.Position
			}
			cursor.unknownDone = len(page) < store.UsageDeliveryPageSize
		} else {
			if cursor.knownDone {
				continue
			}
			page, err := c.Node.Store.KnownUsageDeliveryPage(ctx, c.Node.NodeID, cursor.knownAfter)
			if err != nil {
				return false, err
			}
			if len(page) == 0 {
				cursor.knownDone = true
				continue
			}
			first, err := c.Node.Store.NextControlSequenceRange(ctx, uint64(len(page)))
			if err != nil {
				return false, err
			}
			for i, item := range page {
				record := item.Batch
				if record.UnknownUpper != 0 {
					if err := c.migrateUnknownUsage(ctx, record); err != nil {
						return false, err
					}
					// Newly migrated typed rows must be visited in this scan.
					cursor.unknownDone = false
					cursor.knownAfter = item.Position
					continue
				}
				batch := new(controlv1.UsageBatch)
				if err := proto.Unmarshal(record.Payload, batch); err != nil {
					return false, err
				}
				if batch.BatchId != record.BatchID || batch.BillingSnapshotId != record.SnapshotID {
					return false, fmt.Errorf("agent: durable usage batch metadata mismatch")
				}
				seq := first + uint64(i)
				if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: seq, Body: &controlv1.NodeMessage_UsageBatch{UsageBatch: batch}}); err != nil {
					return false, err
				}
				*lastSequence, cursor.knownAfter = seq, item.Position
			}
			cursor.knownDone = len(page) < store.UsageDeliveryPageSize
		}
		return !cursor.knownDone || !cursor.unknownDone, nil
	}
	return !cursor.knownDone || !cursor.unknownDone, nil
}

// Every control owner receives its own coalesced hint. A configuration-only
// owner does not subscribe; a reconnect's initial durable scan recovers hints
// emitted while disconnected. The periodic replay timer recovers lost hints.
func (n *Node) subscribeUsage() (<-chan struct{}, func(), error) {
	n.usageMu.Lock()
	defer n.usageMu.Unlock()
	if len(n.usageSubscriptions) >= 64 {
		return nil, nil, fmt.Errorf("agent: usage subscription limit exceeded")
	}
	if n.usageSubscriptions == nil {
		n.usageSubscriptions = make(map[chan struct{}]struct{})
	}
	wake := make(chan struct{}, 1)
	n.usageSubscriptions[wake] = struct{}{}
	return wake, func() {
		n.usageMu.Lock()
		delete(n.usageSubscriptions, wake)
		n.usageMu.Unlock()
	}, nil
}

func (n *Node) wakeUsage() {
	n.usageMu.Lock()
	defer n.usageMu.Unlock()
	for wake := range n.usageSubscriptions {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
