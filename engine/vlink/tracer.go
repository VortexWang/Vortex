package vlink

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

type OpenRequest struct {
	RuleID         [16]byte
	RuleRevision   uint64
	PathGeneration uint64
	HopIndex       uint16
	Network        uint8
	Visited        []NodeID
	ClientAddress  []byte
}

type AuthorizeTCP func(context.Context, NodeID, OpenRequest) (net.Conn, uint16)

func MarshalOpen(request OpenRequest) ([]byte, error) {
	if request.RuleID == ([16]byte{}) || request.RuleRevision == 0 || request.PathGeneration == 0 || request.Network != 1 || len(request.Visited) > 16 || (len(request.ClientAddress) != 4 && len(request.ClientAddress) != 16) {
		return nil, fmt.Errorf("%w: invalid TCP OPEN fields", ErrProtocol)
	}
	addressFamily := byte(6)
	if len(request.ClientAddress) == 4 {
		addressFamily = 4
	}
	payload := make([]byte, 0, 38+16*len(request.Visited)+len(request.ClientAddress))
	payload = append(payload, request.RuleID[:]...)
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], request.RuleRevision)
	payload = append(payload, number[:]...)
	binary.BigEndian.PutUint64(number[:], request.PathGeneration)
	payload = append(payload, number[:]...)
	var short [2]byte
	binary.BigEndian.PutUint16(short[:], request.HopIndex)
	payload = append(payload, short[:]...)
	payload = append(payload, request.Network, byte(len(request.Visited)))
	for _, node := range request.Visited {
		payload = append(payload, node[:]...)
	}
	payload = append(payload, addressFamily, byte(len(request.ClientAddress)))
	payload = append(payload, request.ClientAddress...)
	return payload, nil
}

func ParseOpen(payload []byte) (OpenRequest, error) {
	var request OpenRequest
	if len(payload) < 42 {
		return request, fmt.Errorf("%w: short OPEN", ErrProtocol)
	}
	copy(request.RuleID[:], payload[:16])
	request.RuleRevision = binary.BigEndian.Uint64(payload[16:24])
	request.PathGeneration = binary.BigEndian.Uint64(payload[24:32])
	request.HopIndex = binary.BigEndian.Uint16(payload[32:34])
	request.Network = payload[34]
	count := int(payload[35])
	if count > 16 {
		return OpenRequest{}, fmt.Errorf("%w: OPEN visited count exceeds limit", ErrProtocol)
	}
	addressOffset := 36 + count*16
	if len(payload) < addressOffset+2 {
		return OpenRequest{}, fmt.Errorf("%w: truncated OPEN visited list", ErrProtocol)
	}
	request.Visited = make([]NodeID, count)
	for i := range request.Visited {
		copy(request.Visited[i][:], payload[36+i*16:52+i*16])
	}
	family, length := payload[addressOffset], int(payload[addressOffset+1])
	if !((family == 4 && length == 4) || (family == 6 && length == 16)) || len(payload) != addressOffset+2+length {
		return OpenRequest{}, fmt.Errorf("%w: invalid OPEN client address", ErrProtocol)
	}
	request.ClientAddress = append([]byte(nil), payload[addressOffset+2:]...)
	if request.RuleID == ([16]byte{}) || request.RuleRevision == 0 || request.PathGeneration == 0 || request.Network != 1 {
		return OpenRequest{}, fmt.Errorf("%w: invalid OPEN fields", ErrProtocol)
	}
	return request, nil
}

// TraceTCP authenticates a TLS link, opens one locally authorized stream, and
// relays TCP bytes while preserving FIN independently in each direction.
func TraceTCP(ctx context.Context, link *tls.Conn, local net.Conn, hello HelloConfig, request OpenRequest) error {
	if link == nil || local == nil {
		return errors.New("vlink: TLS link and local TCP connection are required")
	}
	peer, err := HandshakeHello(ctx, link, hello)
	if err != nil {
		_ = local.Close()
		forceCloseTLS(link)
		return err
	}
	payload, err := MarshalOpen(request)
	if err != nil {
		_ = local.Close()
		forceCloseTLS(link)
		return err
	}
	allocator := NewStreamIDAllocator(true)
	streamID, err := allocator.Next()
	if err != nil {
		return err
	}
	var writeMu sync.Mutex
	writeFrame := func(frame Frame) error { writeMu.Lock(); defer writeMu.Unlock(); return WriteFrame(link, frame) }
	if err = writeFrame(Frame{Type: FrameOpen, StreamID: streamID, Payload: payload}); err != nil {
		return err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = local.Close(); forceCloseTLS(link) })
	defer stopOnCancel()
	results := make(chan error, 2)
	go func() {
		buffer := make([]byte, 16*1024)
		for {
			n, readErr := local.Read(buffer)
			if n > 0 {
				if writeErr := writeFrame(Frame{Type: FrameData, StreamID: streamID, Payload: append([]byte(nil), buffer[:n]...)}); writeErr != nil {
					results <- writeErr
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					results <- writeFrame(Frame{Type: FrameFIN, StreamID: streamID})
					return
				}
				results <- readErr
				return
			}
		}
	}()
	go func() {
		opened := false
		for {
			frame, readErr := ReadFrame(link)
			if readErr != nil {
				results <- readErr
				return
			}
			if frame.StreamID != streamID {
				results <- fmt.Errorf("%w: unexpected stream id", ErrProtocol)
				return
			}
			switch frame.Type {
			case FrameOpenOK:
				if opened {
					results <- fmt.Errorf("%w: duplicate OPEN_OK", ErrProtocol)
					return
				}
				opened = true
			case FrameOpenErr:
				results <- fmt.Errorf("vlink: remote OPEN rejected (class %d)", binary.BigEndian.Uint16(frame.Payload[:2]))
				return
			case FrameData:
				if !opened {
					results <- fmt.Errorf("%w: DATA before OPEN_OK", ErrProtocol)
					return
				}
				if err := writeAllConn(local, frame.Payload); err != nil {
					results <- err
					return
				}
			case FrameFIN:
				if !opened {
					results <- fmt.Errorf("%w: FIN before OPEN_OK", ErrProtocol)
					return
				}
				closer, ok := local.(interface{ CloseWrite() error })
				if !ok {
					results <- errors.New("vlink: local connection does not support half-close")
					return
				}
				if err := closer.CloseWrite(); err != nil {
					results <- err
					return
				}
				results <- nil
				return
			case FrameRST:
				results <- fmt.Errorf("vlink: remote reset stream (class %d)", binary.BigEndian.Uint16(frame.Payload[:2]))
				return
			default:
				results <- fmt.Errorf("%w: unexpected frame on TCP stream", ErrProtocol)
				return
			}
			_ = peer
		}
	}()
	var first error
	for range 2 {
		current := <-results
		if current != nil && first == nil {
			first = current
			_ = local.Close()
			forceCloseTLS(link)
		}
	}
	forceCloseTLS(link)
	if first != nil {
		_ = local.Close()
	}
	return first
}

// ServeTCP accepts one TCP stream. The authorizer selects and dials the target
// from local policy; the wire request deliberately contains no target address.
func ServeTCP(ctx context.Context, link *tls.Conn, hello HelloConfig, authorize AuthorizeTCP) error {
	if link == nil || authorize == nil {
		return errors.New("vlink: TLS link and TCP authorizer are required")
	}
	peer, err := HandshakeHello(ctx, link, hello)
	if err != nil {
		forceCloseTLS(link)
		return err
	}
	frame, err := ReadFrame(link)
	if err != nil {
		forceCloseTLS(link)
		return err
	}
	if frame.Type != FrameOpen || frame.StreamID%2 != 1 {
		forceCloseTLS(link)
		return fmt.Errorf("%w: expected client OPEN", ErrProtocol)
	}
	request, err := ParseOpen(frame.Payload)
	if err != nil {
		forceCloseTLS(link)
		return err
	}
	target, errorCode := authorize(ctx, peer, request)
	if errorCode != 0 || target == nil {
		if errorCode == 0 {
			errorCode = 2
		}
		response := make([]byte, 18)
		binary.BigEndian.PutUint16(response[:2], errorCode)
		_, _ = rand.Read(response[2:])
		_ = WriteFrame(link, Frame{Type: FrameOpenErr, StreamID: frame.StreamID, Payload: response})
		forceCloseTLS(link)
		return nil
	}
	var writeMu sync.Mutex
	writeFrame := func(value Frame) error { writeMu.Lock(); defer writeMu.Unlock(); return WriteFrame(link, value) }
	if err := writeFrame(Frame{Type: FrameOpenOK, StreamID: frame.StreamID}); err != nil {
		_ = target.Close()
		return err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = target.Close(); forceCloseTLS(link) })
	defer stopOnCancel()
	results := make(chan error, 2)
	go func() {
		for {
			incoming, readErr := ReadFrame(link)
			if readErr != nil {
				results <- readErr
				return
			}
			if incoming.StreamID != frame.StreamID {
				results <- fmt.Errorf("%w: unexpected stream id", ErrProtocol)
				return
			}
			switch incoming.Type {
			case FrameData:
				if err := writeAllConn(target, incoming.Payload); err != nil {
					results <- err
					return
				}
			case FrameFIN:
				closer, ok := target.(interface{ CloseWrite() error })
				if !ok {
					results <- errors.New("vlink: target does not support half-close")
					return
				}
				if err := closer.CloseWrite(); err != nil {
					results <- err
					return
				}
				results <- nil
				return
			case FrameRST:
				results <- errors.New("vlink: stream reset by peer")
				return
			default:
				results <- fmt.Errorf("%w: unexpected frame on TCP stream", ErrProtocol)
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, 16*1024)
		for {
			n, readErr := target.Read(buffer)
			if n > 0 {
				if err := writeFrame(Frame{Type: FrameData, StreamID: frame.StreamID, Payload: append([]byte(nil), buffer[:n]...)}); err != nil {
					results <- err
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					results <- writeFrame(Frame{Type: FrameFIN, StreamID: frame.StreamID})
					return
				}
				results <- readErr
				return
			}
		}
	}()
	var result error
	for range 2 {
		current := <-results
		if current != nil && result == nil {
			result = current
			_ = target.Close()
			forceCloseTLS(link)
		}
	}
	_ = target.Close()
	forceCloseTLS(link)
	return result
}

func writeAllConn(conn net.Conn, payload []byte) error {
	for len(payload) != 0 {
		n, err := conn.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func forceCloseTLS(conn *tls.Conn) {
	if conn != nil {
		_ = conn.NetConn().Close()
	}
}
