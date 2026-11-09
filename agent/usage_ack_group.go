package agent

import (
	"context"
	"fmt"

	controlv1 "vortex/contracts/protobuf/v1"
)

// The receiver validates every envelope's replay identity and current session.
// A group preserves all-or-nothing validation, including unknown rejection.
func (c *ControlClient) applyOrdinaryUsageAcknowledgements(ctx context.Context, messages []*controlv1.ControlMessage) error {
	if c.ControlOnly {
		return fmt.Errorf("agent: usage acknowledgement on configuration-only channel")
	}
	if len(messages) == 0 || len(messages) > 32 {
		return fmt.Errorf("agent: invalid usage acknowledgement group")
	}
	var ids []string
	for _, message := range messages {
		ack := message.GetUsageAck()
		if ack == nil || len(ack.BatchIds) == 0 {
			return fmt.Errorf("agent: usage acknowledgement has no batch IDs")
		}
		ids = append(ids, ack.BatchIds...)
	}
	if err := c.Node.Store.AcknowledgeUsageBatches(ctx, c.Node.NodeID, ids); err != nil {
		return err
	}
	if c.AcknowledgeUsage != nil {
		for _, message := range messages {
			if err := c.AcknowledgeUsage(ctx, message.GetUsageAck().BatchIds); err != nil {
				return err
			}
		}
	}
	return nil
}
