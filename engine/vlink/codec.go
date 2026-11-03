package vlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	HeaderSize     = 8
	MaxPayloadSize = 16 * 1024
	// MaxTLSRecordPayload keeps the complete VLink frame within one TLS 1.3
	// plaintext record when used for TCP DATA frames.
	MaxTLSRecordPayload = MaxPayloadSize - HeaderSize
)

var (
	ErrProtocol           = errors.New("vlink: protocol error")
	ErrFrameTooLarge      = errors.New("vlink: frame payload too large")
	ErrStreamIDsExhausted = errors.New("vlink: stream IDs exhausted")
)

var frameWriteBuffers = sync.Pool{New: func() any {
	buffer := new([HeaderSize + MaxPayloadSize]byte)
	return buffer
}}

type FrameType uint8

const (
	FrameHello   FrameType = 0x01
	FrameOpen    FrameType = 0x02
	FrameOpenOK  FrameType = 0x03
	FrameOpenErr FrameType = 0x04
	FrameData    FrameType = 0x05
	FrameFIN     FrameType = 0x06
	FrameRST     FrameType = 0x07
	FrameDgram   FrameType = 0x08
	FrameWindow  FrameType = 0x09
	FramePing    FrameType = 0x0a
	FramePong    FrameType = 0x0b
	FrameGoAway  FrameType = 0x0c
	framePadding FrameType = 0x0d
)

type Frame struct {
	Type     FrameType
	Flags    uint8
	StreamID uint32
	Payload  []byte
}

func ReadFrame(r io.Reader) (Frame, error) {
	return readFrame(r, nil)
}

// ReadFrameWithBuffer decodes a frame into caller-owned storage. The returned
// payload aliases buffer and remains valid until the caller reuses that buffer.
func ReadFrameWithBuffer(r io.Reader, buffer []byte) (Frame, error) {
	return readFrame(r, buffer)
}

func readFrame(r io.Reader, buffer []byte) (Frame, error) {
	var header [HeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, fmt.Errorf("vlink: read frame header: %w", err)
	}

	typ := FrameType(header[0])
	length := binary.BigEndian.Uint16(header[2:4])
	streamID := binary.BigEndian.Uint32(header[4:8])
	frame := Frame{Type: typ, Flags: header[1], StreamID: streamID}
	if err := validateFrameHeader(frame, length); err != nil {
		return Frame{}, err
	}

	if length == 0 {
		return frame, nil
	}
	if buffer == nil {
		buffer = make([]byte, int(length))
	} else if int(length) > len(buffer) {
		return Frame{}, ErrFrameTooLarge
	}
	frame.Payload = buffer[:int(length)]
	if _, err := io.ReadFull(r, frame.Payload); err != nil {
		return Frame{}, fmt.Errorf("vlink: read frame payload: %w", err)
	}
	return frame, nil
}

func WriteFrame(w io.Writer, frame Frame) error {
	encoded := frameWriteBuffers.Get().(*[HeaderSize + MaxPayloadSize]byte)
	defer frameWriteBuffers.Put(encoded)
	return writeFrameWithBuffer(w, frame, encoded[:])
}

func writeFrameWithBuffer(w io.Writer, frame Frame, encoded []byte) error {
	if len(frame.Payload) > MaxPayloadSize {
		return ErrFrameTooLarge
	}
	if len(encoded) < HeaderSize+len(frame.Payload) {
		return ErrFrameTooLarge
	}
	if err := validateFrameHeader(frame, uint16(len(frame.Payload))); err != nil {
		return err
	}
	encoded[0] = byte(frame.Type)
	encoded[1] = frame.Flags
	binary.BigEndian.PutUint16(encoded[2:4], uint16(len(frame.Payload)))
	binary.BigEndian.PutUint32(encoded[4:8], frame.StreamID)
	copy(encoded[HeaderSize:], frame.Payload)
	if err := writeFull(w, encoded[:HeaderSize+len(frame.Payload)]); err != nil {
		return fmt.Errorf("vlink: write frame: %w", err)
	}
	return nil
}

func validateFrameHeader(frame Frame, length uint16) error {
	if length > MaxPayloadSize {
		return ErrFrameTooLarge
	}
	if frame.Flags != 0 {
		return fmt.Errorf("%w: reserved flags are nonzero", ErrProtocol)
	}
	if frame.Type < FrameHello || frame.Type > FrameGoAway || frame.Type == framePadding {
		return fmt.Errorf("%w: unknown or reserved frame type 0x%02x", ErrProtocol, frame.Type)
	}
	if !validStreamForType(frame.Type, frame.StreamID) {
		return fmt.Errorf("%w: invalid stream ID %d for frame type 0x%02x", ErrProtocol, frame.StreamID, frame.Type)
	}
	switch frame.Type {
	case FrameOpenOK, FrameFIN:
		if length != 0 {
			return fmt.Errorf("%w: frame type 0x%02x requires an empty payload", ErrProtocol, frame.Type)
		}
	case FrameHello:
		if length < 155 || length > 922 {
			return fmt.Errorf("%w: HELLO payload length is outside 155..922", ErrProtocol)
		}
	case FrameOpen:
		if length < 42 || length > 310 {
			return fmt.Errorf("%w: OPEN payload length is outside 42..310", ErrProtocol)
		}
	case FrameOpenErr:
		if length != 18 {
			return fmt.Errorf("%w: OPEN_ERR payload must be 18 bytes", ErrProtocol)
		}
	case FrameData, FrameDgram:
		if length == 0 {
			return fmt.Errorf("%w: frame type 0x%02x requires a non-empty payload", ErrProtocol, frame.Type)
		}
	case FrameRST:
		if length != 2 {
			return fmt.Errorf("%w: RST payload must be 2 bytes", ErrProtocol)
		}
	case FrameWindow:
		if length != 4 {
			return fmt.Errorf("%w: WINDOW payload must be 4 bytes", ErrProtocol)
		}
	case FramePing, FramePong:
		if length != 8 {
			return fmt.Errorf("%w: PING/PONG payload must be 8 bytes", ErrProtocol)
		}
	case FrameGoAway:
		if length != 6 {
			return fmt.Errorf("%w: GOAWAY payload must be 6 bytes", ErrProtocol)
		}
	}
	return nil
}

func validStreamForType(typ FrameType, streamID uint32) bool {
	switch typ {
	case FrameHello, FramePing, FramePong, FrameGoAway:
		return streamID == 0
	case FrameOpen, FrameOpenOK, FrameOpenErr, FrameData, FrameFIN, FrameRST, FrameDgram:
		return streamID != 0
	case FrameWindow:
		return true
	default:
		return false
	}
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return errors.New("vlink: writer returned invalid byte count")
		}
		p = p[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

type StreamIDAllocator struct {
	next      uint32
	exhausted bool
}

func NewStreamIDAllocator(clientInitiated bool) StreamIDAllocator {
	if clientInitiated {
		return StreamIDAllocator{next: 1}
	}
	return StreamIDAllocator{next: 2}
}

func (a *StreamIDAllocator) Next() (uint32, error) {
	if a.exhausted || a.next == 0 {
		return 0, ErrStreamIDsExhausted
	}
	id := a.next
	if id > ^uint32(0)-2 {
		a.exhausted = true
	} else {
		a.next += 2
	}
	return id, nil
}
