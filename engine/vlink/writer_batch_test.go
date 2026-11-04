package vlink

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type batchRecordingConn struct {
	net.Conn
	bytes.Buffer
	writes int
}

func TestLimitedWindowClosesInsteadOfLosingCredit(t *testing.T) {
	local, peer := net.Pipe()
	limits := defaultFrameLimits()
	limits[FrameWindow] = FrameLimit{Rate: 0.001, Burst: 1}
	link, err := newMuxLink(local, MuxConfig{AggregateWindow: 4, MaxWindow: 16, Limits: LimitConfig{ByType: limits}})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	defer link.Close(io.EOF)
	peer.SetWriteDeadline(time.Now().Add(time.Second))
	for i := 0; i < 2; i++ {
		if err := WriteFrame(peer, Frame{Type: FrameWindow, Payload: []byte{0, 0, 0, 1}}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-link.Closed():
		if !errors.Is(link.Err(), ErrRateLimited) {
			t.Fatalf("unexpected close: %v", link.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("rate-limited credit was silently discarded")
	}
}

func (c *batchRecordingConn) Read(p []byte) (int, error) { return c.Buffer.Read(p) }

func (c *batchRecordingConn) Write(p []byte) (int, error) {
	c.writes++
	return c.Buffer.Write(p)
}

func TestDataWriteGroupPreservesWireBoundariesAndOrder(t *testing.T) {
	conn := &batchRecordingConn{}
	link := &MuxLink{conn: conn, dataWrites: make(chan frameWrite, 2)}
	firstPayload := bytes.Repeat([]byte{7}, 4*MaxTLSRecordPayload)
	secondPayload := bytes.Repeat([]byte{9}, MaxTLSRecordPayload+1)
	first := frameWrite{ctx: context.Background(), frame: Frame{Type: FrameData, StreamID: 1, Payload: firstPayload}, done: make(chan error, 1)}
	second := frameWrite{ctx: context.Background(), frame: Frame{Type: FrameData, StreamID: 3, Payload: secondPayload}, done: make(chan error, 1)}
	link.dataWrites <- second
	var encoded bytes.Buffer
	if !link.sendDataBatch(first, &encoded) {
		t.Fatal("batch failed")
	}
	if err := <-first.done; err != nil {
		t.Fatal(err)
	}
	if err := <-second.done; err != nil {
		t.Fatal(err)
	}
	if conn.writes != 1 {
		t.Fatalf("transport calls=%d, want 1", conn.writes)
	}
	decoded := map[uint32][]byte{}
	for {
		frame, err := ReadFrame(&conn.Buffer)
		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type != FrameData || len(frame.Payload) > MaxTLSRecordPayload {
			t.Fatal("invalid wire frame")
		}
		decoded[frame.StreamID] = append(decoded[frame.StreamID], frame.Payload...)
	}
	if !bytes.Equal(decoded[1], firstPayload) || !bytes.Equal(decoded[3], secondPayload) {
		t.Fatal("batch corrupted or reordered bytes")
	}
}
