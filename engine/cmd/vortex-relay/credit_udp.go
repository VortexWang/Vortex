package main

import (
	"context"
	"io"
	core "vortex/engine/accounting"
)

// authorizedDatagram reserves before a write. Only the local VLink send queue
// can report a definite zero-byte refusal; network short writes are unknown.
func authorizedDatagram(ctx context.Context, s core.Session, d core.Direction, p []byte, queueRefusal bool, write func([]byte) (int, error)) (bool, error) {
	var b *core.Batch
	if s != nil {
		var err error
		b, err = s.Reserve(ctx, d, int64(len(p)))
		if err != nil {
			return false, err
		}
	}
	n, err := write(p)
	if err == nil && n == 0 && queueRefusal {
		if b != nil {
			if err = b.Return(ctx); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if err != nil || n != len(p) {
		if b != nil {
			if resolveErr := b.MarkUnknown(ctx); resolveErr != nil {
				return false, resolveErr
			}
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		return false, err
	}
	if b != nil {
		if err = b.Commit(ctx, int64(n)); err != nil {
			return false, err
		}
	}
	return true, nil
}
