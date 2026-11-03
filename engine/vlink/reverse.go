package vlink

import (
	"context"
	"errors"
	"sync"
	"time"
)

type ReverseDialFunc func(context.Context) (*MuxLink, error)
type RuleDeclarationFunc func(context.Context, *MuxLink) error

type ReverseConnector struct {
	dial       ReverseDialFunc
	declare    RuleDeclarationFunc
	backoff    Backoff
	mu         sync.Mutex
	current    *MuxLink
	changed    chan struct{}
	startOnce  sync.Once
	done       chan struct{}
	startError error
	cancel     context.CancelFunc
}

func NewReverseConnector(dial ReverseDialFunc, declare RuleDeclarationFunc, backoff Backoff) (*ReverseConnector, error) {
	if dial == nil || declare == nil {
		return nil, errors.New("vlink: reverse connector requires dial and rule declaration callbacks")
	}
	if backoff.Initial <= 0 {
		backoff.Initial = 250 * time.Millisecond
	}
	if backoff.Maximum <= 0 {
		backoff.Maximum = 30 * time.Second
	}
	return &ReverseConnector{dial: dial, declare: declare, backoff: backoff, changed: make(chan struct{}), done: make(chan struct{})}, nil
}

// Run repeatedly dials the configured ingress through the injected TLS/HELLO
// factory. Rule declaration happens only after that factory returns an
// authenticated MuxLink.
func (r *ReverseConnector) Run(ctx context.Context) error {
	r.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		r.mu.Lock()
		r.cancel = cancel
		r.mu.Unlock()
		go r.run(runCtx)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return r.startError
	}
}

func (r *ReverseConnector) Current() (*MuxLink, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current, r.current != nil
}

func (r *ReverseConnector) WaitReady(ctx context.Context) (*MuxLink, error) {
	for {
		r.mu.Lock()
		if r.current != nil {
			link := r.current
			r.mu.Unlock()
			return link, nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.done:
			return nil, errors.New("vlink: reverse connector stopped")
		case <-changed:
		}
	}
}

func (r *ReverseConnector) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	if r.current != nil {
		r.current.Close(ErrProtocol)
		r.current = nil
	}
	r.broadcastLocked()
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ReopenTCP makes retries an explicit upper-layer decision. UDP is never
// replayed, and TCP is retried only after the caller's idempotency check.
func (r *ReverseConnector) ReopenTCP(ctx context.Context, request OpenRequest, idempotent func(OpenRequest) bool) (*MuxStream, error) {
	if request.Network != uint8(StreamTCP) || idempotent == nil || !idempotent(request) {
		return nil, errors.New("vlink: stream is not approved for TCP re-open")
	}
	link, ok := r.Current()
	if !ok {
		return nil, errors.New("vlink: reverse link unavailable")
	}
	return link.Open(ctx, request)
}

func (r *ReverseConnector) run(ctx context.Context) {
	defer close(r.done)
	for {
		if err := ctx.Err(); err != nil {
			r.Stop()
			return
		}
		link, err := r.dial(ctx)
		if err == nil && link != nil {
			err = r.declare(ctx, link)
		}
		if err == nil && link != nil {
			r.mu.Lock()
			r.current = link
			r.backoff.Attempt = 0
			r.broadcastLocked()
			r.mu.Unlock()
			select {
			case <-ctx.Done():
			case <-link.Closed():
			}
			r.mu.Lock()
			if r.current == link {
				r.current = nil
			}
			r.broadcastLocked()
			r.mu.Unlock()
			link.Close(ErrProtocol)
			if !r.waitBackoff(ctx) {
				r.Stop()
				return
			}
			continue
		}
		if link != nil {
			link.Close(ErrProtocol)
		}
		if ctx.Err() != nil {
			r.Stop()
			return
		}
		if !r.waitBackoff(ctx) {
			r.Stop()
			return
		}
	}
}

func (r *ReverseConnector) waitBackoff(ctx context.Context) bool {
	timer := time.NewTimer(r.backoff.Next())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *ReverseConnector) broadcastLocked() { close(r.changed); r.changed = make(chan struct{}) }
