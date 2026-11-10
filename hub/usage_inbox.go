package hub

import (
	"context"

	controlv1 "vortex/contracts/protobuf/v1"
)

type nodeReceive struct {
	m   *controlv1.NodeMessage
	err error
}

// The receiver owns Recv; the control loop owns all sends and authority changes.
// Only already-arrived adjacent known reports are grouped. Commands and unknown
// reports preserve order and retain their separate validation/ACK semantics.
type nodeInbox struct {
	ctx  context.Context
	wire chan nodeReceive
	next *nodeReceive
}

func newNodeInbox(ctx context.Context, receive func() (*controlv1.NodeMessage, error)) *nodeInbox {
	i := &nodeInbox{ctx: ctx, wire: make(chan nodeReceive, 32)}
	go func() {
		for {
			m, err := receive()
			select {
			case i.wire <- nodeReceive{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return i
}

func (i *nodeInbox) receive() (*controlv1.NodeMessage, error) {
	if i.next != nil {
		next := i.next
		i.next = nil
		return next.m, next.err
	}
	select {
	case r := <-i.wire:
		return r.m, r.err
	case <-i.ctx.Done():
		return nil, i.ctx.Err()
	}
}

func (i *nodeInbox) known(first *controlv1.NodeMessage, validate func(*controlv1.NodeMessage) error) ([]*controlv1.NodeMessage, error) {
	messages := []*controlv1.NodeMessage{first}
	for len(messages) < 32 {
		select {
		case candidate := <-i.wire:
			if candidate.err != nil || candidate.m.GetUsageBatch() == nil {
				i.next = &candidate
				return messages, nil
			}
			if err := validate(candidate.m); err != nil {
				return nil, err
			}
			messages = append(messages, candidate.m)
		default:
			return messages, nil
		}
	}
	return messages, nil
}
