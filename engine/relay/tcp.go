package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"vortex/engine/accounting"
)

// Larger gated chunks reduce accounting and frame scheduling overhead while
// remaining below the VLink payload aggregation limit.
const bufferSize = 128 * 1024

var ErrShortWrite = errors.New("relay: short write")

type Direction string

const (
	LeftToRight Direction = "left_to_right"
	RightToLeft Direction = "right_to_left"
)

// Meter may be called concurrently and receives bytes accepted by a destination write.
type Meter func(Direction, int64) error

type Stats struct {
	LeftToRight int64
	RightToLeft int64
}

type copyResult struct {
	direction Direction
	bytes     int64
	err       error
}

// TCP forwards both directions. EOF closes only the corresponding write side;
// cancellation and other I/O errors close both connections.
func TCP(ctx context.Context, left, right net.Conn, meter Meter) (Stats, error) {
	return tcp(ctx, left, right, meter, nil)
}

// TCPWithGate opens an in-process policy session before relaying. Policy
// sessions reserve each read chunk before destination writes and resolve usage
// using the bytes accepted by those writes. Linux splice is intentionally not
// selected for gated traffic because reservations must precede socket writes.
func TCPWithGate(ctx context.Context, left, right net.Conn, gate accounting.Gate, meter Meter) (Stats, error) {
	if gate == nil {
		return Stats{}, errors.New("relay: TCP gate is required")
	}
	session, err := gate.Open(ctx, accounting.Flow{Key: left.RemoteAddr().String() + "->" + right.RemoteAddr().String()})
	if err != nil {
		_ = left.Close()
		_ = right.Close()
		return Stats{}, fmt.Errorf("relay: open TCP gate: %w", err)
	}
	if session == nil {
		_ = left.Close()
		_ = right.Close()
		return Stats{}, errors.New("relay: TCP gate returned a nil session")
	}
	stats, relayErr := tcp(ctx, left, right, meter, session)
	if closeErr := session.Close(); closeErr != nil {
		relayErr = errors.Join(relayErr, fmt.Errorf("relay: close TCP gate: %w", closeErr))
	}
	return stats, relayErr
}

func tcp(ctx context.Context, left, right net.Conn, meter Meter, gate accounting.Session) (Stats, error) {
	var stats Stats
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = left.Close()
			_ = right.Close()
		})
	}
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			closeBoth()
		case <-stopWatch:
		}
	}()
	defer func() {
		close(stopWatch)
		<-watchDone
	}()

	results := make(chan copyResult, 2)
	copyDirection := func(src, dst net.Conn, direction Direction) {
		var n int64
		var err error
		if gate != nil {
			n, err = copyTCPGated(ctx, src, dst, direction, meter, gate)
		} else {
			n, err = copyTCPPlatform(src, dst, direction, meter)
		}
		if err != nil && !errors.Is(err, net.ErrClosed) {
			closeBoth()
		}
		results <- copyResult{direction: direction, bytes: n, err: err}
	}
	go copyDirection(left, right, LeftToRight)
	go copyDirection(right, left, RightToLeft)

	var firstErr error
	for range 2 {
		result := <-results
		switch result.direction {
		case LeftToRight:
			stats.LeftToRight = result.bytes
		case RightToLeft:
			stats.RightToLeft = result.bytes
		}
		if result.err != nil && !errors.Is(result.err, net.ErrClosed) && firstErr == nil {
			firstErr = result.err
		}
	}
	if ctx.Err() != nil && firstErr == nil {
		firstErr = ctx.Err()
	}
	return stats, firstErr
}

func copyTCPGated(ctx context.Context, src, dst net.Conn, direction Direction, meter Meter, session accounting.Session) (int64, error) {
	buf := make([]byte, bufferSize)
	var total int64
	for {
		nr, readErr := src.Read(buf)
		if nr > 0 {
			batch, err := session.Reserve(ctx, accounting.Direction(direction), int64(nr))
			if err != nil {
				return total, fmt.Errorf("reserve %s: %w", direction, err)
			}
			if batch == nil || batch.Reserved() < int64(nr) {
				if batch != nil {
					_ = batch.MarkUnknown(ctx)
				}
				return total, errors.New("relay: gate returned an undersized TCP reservation")
			}
			written := 0
			for written < nr {
				nw, writeErr := dst.Write(buf[written:nr])
				if nw > 0 {
					written += nw
					total += int64(nw)
				}
				if writeErr != nil {
					return total, errors.Join(writeErr, resolveBatch(ctx, batch, written))
				}
				if nw == 0 {
					return total, errors.Join(ErrShortWrite, resolveBatch(ctx, batch, written))
				}
			}
			if err := resolveBatch(ctx, batch, written); err != nil {
				return total, fmt.Errorf("resolve %s usage: %w", direction, err)
			}
			if meter != nil && written > 0 {
				if err := meter(direction, int64(written)); err != nil {
					return total, fmt.Errorf("meter %s: %w", direction, err)
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return total, readErr
			}
			if errors.Is(readErr, io.EOF) {
				if closer, ok := dst.(interface{ CloseWrite() error }); ok {
					return total, closer.CloseWrite()
				}
				return total, errors.New("relay: destination does not support half-close")
			}
			return total, readErr
		}
	}
}

func resolveBatch(ctx context.Context, batch *accounting.Batch, used int) error {
	if used == 0 {
		return batch.Return(ctx)
	}
	return batch.Commit(ctx, int64(used))
}

func copyTCP(src, dst net.Conn, direction Direction, meter Meter) (int64, error) {
	buf := make([]byte, bufferSize)
	var total int64
	for {
		nr, readErr := src.Read(buf)
		written := 0
		for written < nr {
			nw, writeErr := dst.Write(buf[written:nr])
			if nw > 0 {
				written += nw
				total += int64(nw)
				if meter != nil {
					if err := meter(direction, int64(nw)); err != nil {
						return total, fmt.Errorf("meter %s: %w", direction, err)
					}
				}
			}
			if writeErr != nil {
				return total, writeErr
			}
			if nw == 0 {
				return total, ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return total, readErr
			}
			if errors.Is(readErr, io.EOF) {
				if closer, ok := dst.(interface{ CloseWrite() error }); ok {
					return total, closer.CloseWrite()
				}
				return total, errors.New("relay: destination does not support half-close")
			}
			return total, readErr
		}
	}
}
