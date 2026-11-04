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
	request, err := m.enqueueWrite(ctx, frame)
	if err != nil {
		return err
	}
	return m.waitWrite(request)
}

func (m *MuxLink) enqueueWrite(ctx context.Context, frame Frame) (frameWrite, error) {
	if err := ctx.Err(); err != nil {
		return frameWrite{}, err
	}
	request := frameWrite{ctx: ctx, frame: frame, done: make(chan error, 1)}
	queue := m.controlWrites
	if frame.Type == FrameData {
		queue = m.dataWrites
	}
	select {
	case queue <- request:
	case <-ctx.Done():
		return frameWrite{}, ctx.Err()
	case <-m.done:
		return frameWrite{}, m.Err()
	}
	m.wakeWriter()
	return request, nil
}
func (m *MuxLink) waitWrite(request frameWrite) error {
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
			if !m.sendDataBatch(request, &encoded) {
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

// sendDataBatch writes several already-windowed DATA frames through one
// net.Conn/TLS call. Each request is completed only after the whole batch is
// accepted, so a partial transport failure conservatively resolves every
// affected reservation as unknown through the caller's error path.
func (m *MuxLink) sendDataBatch(first frameWrite, encoded *bytes.Buffer) bool {
	encoded.Reset()
	requests := [8]frameWrite{first}
	count := 1
	for count < len(requests) {
		select {
		case request := <-m.dataWrites:
			requests[count] = request
			count++
		default:
			goto ready
		}
	}
ready:
	var deadline time.Time
	for i := 0; i < count; i++ {
		if d, ok := requests[i].ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
			deadline = d
		}
	}
	if !deadline.IsZero() {
		_ = m.conn.SetWriteDeadline(deadline)
		defer m.conn.SetWriteDeadline(time.Time{})
	}
	for i := 0; i < count; i++ {
		if err := requests[i].ctx.Err(); err != nil {
			for j := 0; j < count; j++ {
				requests[j].done <- err
			}
			return true
		}
		payload := requests[i].frame.Payload
		for len(payload) > 0 {
			size := len(payload)
			if size > MaxTLSRecordPayload {
				size = MaxTLSRecordPayload
			}
			frame := requests[i].frame
			frame.Payload = payload[:size]
			if err := WriteFrame(encoded, frame); err != nil {
				for j := 0; j < count; j++ {
					requests[j].done <- err
				}
				m.Close(err)
				return false
			}
			payload = payload[size:]
		}
	}
	if err := writeFull(m.conn, encoded.Bytes()); err != nil {
		for i := 0; i < count; i++ {
			requests[i].done <- err
		}
		m.Close(err)
		return false
	}
	for i := 0; i < count; i++ {
		requests[i].done <- nil
		if m.observer != nil {
			size := len(requests[i].frame.Payload)
			for size > 0 {
				chunk := size
				if chunk > MaxTLSRecordPayload {
					chunk = MaxTLSRecordPayload
				}
				m.observer.Frame(m.peerRole(), FrameData, 2, chunk)
				size -= chunk
			}
		}
	}
	return true
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
