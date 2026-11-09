package vlink

import (
	"context"
	"time"
)

// ReceiveBatch waits for one frame, then takes only frames already queued. It
// introduces no batching delay and preserves FIN/RST ordering. The caller owns
// the returned frames and must Release each after processing.
func (s *MuxStream) ReceiveBatch(ctx context.Context, frames []Frame) (int, error) {
	if s.Network != StreamTCP || len(frames) == 0 || len(frames) > 4 {
		return 0, ErrProtocol
	}
	first, err := s.Receive(ctx)
	if err != nil {
		return 0, err
	}
	frames[0] = first
	n := 1
	for n < len(frames) {
		if s.tcpReceive != nil {
			frame, ok := s.tcpReceive.pop()
			if !ok {
				return n, nil
			}
			frames[n] = frame
			n++
			continue
		}
		select {
		case frame := <-s.receive:
			frames[n] = frame
			n++
		default:
			return n, nil
		}
	}
	return n, nil
}

// WriteDataFrames pipelines up to four already-decoded DATA frames without
// concatenating or copying their payloads. Per-frame credit and synchronous
// write completion remain the execution boundary.
func (s *MuxStream) WriteDataFrames(ctx context.Context, frames []Frame) error {
	if s.Network != StreamTCP || len(frames) == 0 || len(frames) > 4 {
		return ErrProtocol
	}
	for _, frame := range frames {
		if frame.Type != FrameData || len(frame.Payload) == 0 || len(frame.Payload) > MaxTLSRecordPayload {
			return ErrProtocol
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeClosed.Load() {
		return ErrStreamState
	}
	var pending [4]frameWrite
	for i, frame := range frames {
		if err := s.reserveEarlyData(ctx, len(frame.Payload)); err != nil {
			// Earlier frames may already be owned by the TLS writer. Join it
			// before the caller can Release or reuse those payloads.
			return s.stopDataWrite(err)
		}
		started := time.Now()
		if err := s.link.windows.Acquire(ctx, s.ID, uint32(len(frame.Payload))); err != nil {
			return s.stopDataWrite(err)
		}
		if s.link.observer != nil {
			s.link.observer.WindowWait(time.Since(started))
		}
		frame.StreamID = s.ID
		request, err := s.link.enqueueWrite(ctx, frame)
		if err != nil {
			return s.stopDataWrite(err)
		}
		pending[i] = request
	}
	for i := range frames {
		if err := s.link.waitWrite(pending[i]); err != nil {
			return s.stopDataWrite(err)
		}
	}
	return nil
}

func (s *MuxStream) stopDataWrite(err error) error {
	s.link.Close(err)
	<-s.link.writerStopped
	return err
}
