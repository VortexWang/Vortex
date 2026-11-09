package vlink

import (
	"bytes"
	"testing"
)

func TestTCPReceiveQueueCompactsWithinByteWindowWithoutChangingOwnedFrames(t *testing.T) {
	const window = 3*MaxTLSRecordPayload + 17
	q := newTCPReceiveQueue(window, 2)
	var want []byte
	for offset := 0; offset < window; {
		n := 113
		if n > window-offset {
			n = window - offset
		}
		payload := make([]byte, n)
		for i := range payload {
			payload[i] = byte((offset + i) * 17)
		}
		want = append(want, payload...)
		if err := q.push(Frame{Type: FrameData, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		offset += n
	}
	if err := q.push(Frame{Type: FrameData, Payload: []byte{1}}); err == nil {
		t.Fatal("exceeded byte bound")
	}
	if err := q.push(Frame{Type: FrameFIN}); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		f, ok := q.pop()
		if !ok {
			t.Fatal("lost FIN")
		}
		if f.Type == FrameFIN {
			f.Release()
			break
		}
		got = append(got, f.Payload...)
		f.Release()
	}
	if !bytes.Equal(got, want) {
		t.Fatal("compaction changed DATA")
	}
	if q.bytes != 0 || q.count != 0 {
		t.Fatal("queue did not drain")
	}
	// A popped lease may be held by a concurrent target write. Subsequent
	// compaction must never recycle or mutate it before that owner releases it.
	first := Frame{Type: FrameData, Payload: []byte("held-payload")}
	if err := q.push(first); err != nil {
		t.Fatal(err)
	}
	held, _ := q.pop()
	defer held.Release()
	for i := 0; i < 200; i++ {
		if err := q.push(Frame{Type: FrameData, Payload: []byte("new")}); err != nil {
			t.Fatal(err)
		}
	}
	if string(held.Payload) != "held-payload" {
		t.Fatal("mutated popped frame")
	}
	for {
		f, ok := q.pop()
		if !ok {
			break
		}
		f.Release()
	}
}
