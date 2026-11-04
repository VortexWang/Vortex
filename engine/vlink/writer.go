package vlink

import (
	"bytes"
	"context"
	"time"
)

type frameWrite struct {
	ctx   context.Context
	frame Frame
	done  chan error
}

// write keeps DATA synchronous at the accounting execution boundary. Both
// application queues are bounded; UDP admission remains nonblocking.
func (m *MuxLink) write(ctx context.Context, frame Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request := frameWrite{ctx: ctx, frame: frame, done: make(chan error, 1)}
	queue := m.controlWrites
	if frame.Type == FrameData {
		queue = m.dataWrites
	}
	select {
	case queue <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return m.Err()
	}
	m.wakeWriter()
	// After admission the writer owns the request until it resolves. A partial
	// transport failure is propagated to the caller's unknown-outcome handling.
	select {
	case err := <-request.done:
		return err
	case <-m.done:
		return m.Err()
	}
}

func (m *MuxLink) wakeWriter() {
	select {
	case m.writeWake <- struct{}{}:
	default:
	}
}

// dgramLoop is the sole TLS writer. Control frames have priority; bulk DATA
// and DGRAM alternate bounded turns. Only datagrams already queued are batched,
// so an idle UDP flow incurs no batching timer or artificial latency.
func (m *MuxLink) dgramLoop() {
	udpTurn := true
	var encoded bytes.Buffer
	encoded.Grow(MaxPayloadSize + HeaderSize)
	for {
		select {
		case <-m.done:
			return
		default:
		}
		select {
		case request := <-m.controlWrites:
			if !m.sendRequest(request) {
				return
			}
			continue
		default:
		}
		if udpTurn {
			if m.sendDatagrams(&encoded) {
				udpTurn = false
				continue
			}
		}
		select {
		case request := <-m.dataWrites:
			if !m.sendRequest(request) {
				return
			}
			udpTurn = true
			continue
		default:
		}
		if m.sendDatagrams(&encoded) {
			udpTurn = false
			continue
		}
		select {
		case <-m.writeWake:
		case <-m.done:
			return
		}
	}
}

func (m *MuxLink) sendRequest(request frameWrite) bool {
	if err := request.ctx.Err(); err != nil {
		request.done <- err
		return true
	}
	if deadline, ok := request.ctx.Deadline(); ok {
		_ = m.conn.SetWriteDeadline(deadline)
		defer m.conn.SetWriteDeadline(time.Time{})
	}
	err := WriteFrame(m.conn, request.frame)
	request.done <- err
	if err != nil {
		m.Close(err)
		return false
	}
	if err == nil && m.observer != nil {
		m.observer.Frame(m.peerRole(), request.frame.Type, 2, len(request.frame.Payload))
	}
	return true
}

func (m *MuxLink) sendDatagrams(encoded *bytes.Buffer) bool {
	encoded.Reset()
	// 13 maximum-sized frames at most; never monopolize the writer. Typical
	// 1200-byte DGRAMs fit thirteen to a TLS record instead of one per write.
	for i := 0; i < 13 && encoded.Len() < MaxTLSRecordPayload; i++ {
		remaining := 0
		if encoded.Len() > 0 {
			remaining = HeaderSize + MaxPayloadSize - encoded.Len()
		}
		item, ok := m.dgrams.TryNextWithin(remaining)
		if !ok {
			break
		}
		frame := Frame{Type: FrameDgram, StreamID: item.StreamID, Payload: item.Payload}
		if err := WriteFrame(encoded, frame); err != nil {
			m.Close(err)
			return true
		}
		if m.observer != nil {
			m.observer.Frame(m.peerRole(), frame.Type, 2, len(frame.Payload))
		}
	}
	if encoded.Len() == 0 {
		return false
	}
	if err := writeFull(m.conn, encoded.Bytes()); err != nil {
		m.Close(err)
	}
	return true
}
