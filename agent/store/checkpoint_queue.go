package store

import (
	"context"
	"sync"
	"time"
)

type checkpointRequest struct {
	ctx     context.Context
	entries []RuntimeBlockCheckpoint
	result  chan error
}
type checkpointQueue struct {
	mu      sync.Mutex
	pending []*checkpointRequest
	count   int
	running bool
}

func (s *Store) enqueueCheckpoints(ctx context.Context, entries []RuntimeBlockCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q := &s.checkpoints
	q.mu.Lock()
	if q.count >= 64 {
		q.mu.Unlock()
		return ErrAuthorizationDenied
	}
	r := &checkpointRequest{ctx: ctx, entries: entries, result: make(chan error, 1)}
	q.pending = append(q.pending, r)
	q.count++
	if !q.running {
		q.running = true
		go s.drainCheckpoints()
	}
	q.mu.Unlock()
	// The caller retains ownership of entries until their durable outcome is known.
	return <-r.result
}

func (s *Store) drainCheckpoints() {
	timer := time.NewTimer(time.Millisecond)
	<-timer.C
	q := &s.checkpoints
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		count, size := 0, 0
		for _, r := range q.pending {
			if size+len(r.entries) > 64 {
				break
			}
			size += len(r.entries)
			count++
		}
		group := append([]*checkpointRequest(nil), q.pending[:count]...)
		q.pending = q.pending[count:]
		q.mu.Unlock()
		err := s.commitCheckpointRequests(group)
		q.mu.Lock()
		q.count -= count
		q.mu.Unlock()
		for _, r := range group {
			r.result <- err
		}
	}
}

func (s *Store) commitCheckpointRequests(group []*checkpointRequest) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks sync.WaitGroup
	var stops []func() bool
	defer func() {
		for _, stop := range stops {
			if stop() {
				callbacks.Done()
			}
		}
		callbacks.Wait()
	}()
	var entries []RuntimeBlockCheckpoint
	seen := map[[2]string]bool{}
	for _, r := range group {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		callbacks.Add(1)
		stops = append(stops, context.AfterFunc(r.ctx, func() { cancel(); callbacks.Done() }))
		for _, entry := range r.entries {
			key := [2]string{entry.Block.NodeID, entry.CheckpointID}
			if seen[key] {
				return ErrUsageBatchConflict
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}
	return s.commitRuntimeCheckpoints(ctx, entries)
}
