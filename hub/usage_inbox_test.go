package hub

import (
	"context"
	"errors"
	"io"
	"testing"

	controlv1 "vortex/contracts/protobuf/v1"
)

func TestUsageInboxPreservesCommandAndEOFOrder(t *testing.T) {
	known := func(seq uint64) *controlv1.NodeMessage {
		return &controlv1.NodeMessage{Sequence: seq, Body: &controlv1.NodeMessage_UsageBatch{UsageBatch: &controlv1.UsageBatch{BatchId: "batch"}}}
	}
	inbox := &nodeInbox{ctx: context.Background(), wire: make(chan nodeReceive, 32)}
	command := &controlv1.NodeMessage{Sequence: 3, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{}}}
	inbox.wire <- nodeReceive{m: known(2)}
	inbox.wire <- nodeReceive{m: command}
	inbox.wire <- nodeReceive{m: known(4)}
	inbox.wire <- nodeReceive{err: io.EOF}
	last := uint64(1)
	validate := func(m *controlv1.NodeMessage) error {
		if m.Sequence <= last {
			return errors.New("replay")
		}
		last = m.Sequence
		return nil
	}
	group, err := inbox.known(known(1), validate)
	if err != nil || len(group) != 2 || last != 2 {
		t.Fatal(group, last, err)
	}
	got, err := inbox.receive()
	if err != nil || got != command {
		t.Fatal("command reordered", got, err)
	}
	got, err = inbox.receive()
	if err != nil || got.Sequence != 4 {
		t.Fatal(got, err)
	}
	group, err = inbox.known(got, validate)
	if err != nil || len(group) != 1 {
		t.Fatal(group, err)
	}
	if _, err = inbox.receive(); err != io.EOF {
		t.Fatal("EOF lost", err)
	}
	inbox.wire <- nodeReceive{m: known(1)}
	if _, err = inbox.known(known(5), validate); err == nil {
		t.Fatal("group bypassed replay validation")
	}
}
