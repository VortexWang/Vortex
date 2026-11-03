package vlink

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	want := Frame{Type: FrameData, StreamID: 7, Payload: []byte("payload")}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.Flags != want.Flags || got.StreamID != want.StreamID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("round trip mismatch: got %#v want %#v", got, want)
	}
}

func TestReadFrameRejectsMalformedHeadersBeforePayload(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   error
	}{
		{"unknown type", []byte{0xff, 0, 0, 0, 0, 0, 0, 0}, ErrProtocol},
		{"reserved flags", []byte{byte(FrameData), 1, 0, 0, 0, 0, 0, 1}, ErrProtocol},
		{"oversized payload", []byte{byte(FrameData), 0, 0x40, 1, 0, 0, 0, 1}, ErrFrameTooLarge},
		{"invalid stream", []byte{byte(FrameData), 0, 0, 0, 0, 0, 0, 0}, ErrProtocol},
		{"invalid control stream", []byte{byte(FramePing), 0, 0, 0, 0, 0, 0, 1}, ErrProtocol},
		{"empty open", []byte{byte(FrameOpen), 0, 0, 0, 0, 0, 0, 1}, ErrProtocol},
		{"short hello", []byte{byte(FrameHello), 0, 0, 154, 0, 0, 0, 0}, ErrProtocol},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(tt.header))
			if !errors.Is(err, tt.want) {
				t.Fatalf("ReadFrame() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestReadFrameRejectsTruncatedHeaderAndPayload(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader([]byte{byte(FramePing)})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header error = %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{byte(FrameData), 0, 0, 2, 0, 0, 0, 1, 'x'})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated payload error = %v", err)
	}
}

func TestStreamIDAllocatorParityMonotonicAndExhaustion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		initiator bool
		first     uint32
	}{{"odd", true, 1}, {"even", false, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewStreamIDAllocator(tc.initiator)
			first, err := a.Next()
			if err != nil || first != tc.first {
				t.Fatalf("first ID = %d, %v", first, err)
			}
			second, err := a.Next()
			if err != nil || second != first+2 {
				t.Fatalf("second ID = %d, %v", second, err)
			}
		})
	}
	a := StreamIDAllocator{next: ^uint32(0)}
	last, err := a.Next()
	if err != nil || last != ^uint32(0) {
		t.Fatalf("last odd stream ID = %d, %v", last, err)
	}
	if _, err := a.Next(); !errors.Is(err, ErrStreamIDsExhausted) {
		t.Fatalf("exhausted allocator error = %v", err)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	err := WriteFrame(io.Discard, Frame{Type: FrameData, StreamID: 1, Payload: make([]byte, MaxPayloadSize+1)})
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame() error = %v", err)
	}
}
