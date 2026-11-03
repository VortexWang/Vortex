package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const bufferSize = 32 * 1024

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
		n, err := copyTCPPlatform(src, dst, direction, meter)
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
