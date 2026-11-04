package link

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"sync"
	"time"

	"vortex/engine/vlink"
)

// Transport owns outgoing authenticated links. TCP pool leases are exclusive;
// UDP always uses a separate shared mux pool, never an exclusive TCP lease.
type Transport struct {
	mode     vlink.TransportMode
	tcp, udp *vlink.LinkPool
	closed   chan struct{}
	once     sync.Once
}

type pooledLink struct {
	mux   *vlink.MuxLink
	peer  vlink.NodeID
	owner *Supervisor
	once  sync.Once
}

func (p *pooledLink) Healthy() bool {
	select {
	case <-p.mux.Closed():
		return false
	default:
		return true
	}
}
func (p *pooledLink) Close() error { p.once.Do(func() { p.owner.untrack(p.mux) }); return nil }

func NewTransport(ctx context.Context, owner *Supervisor, mode vlink.TransportMode, bounds vlink.PoolConfig) (*Transport, error) {
	if owner == nil || (mode != vlink.ModePool && mode != vlink.ModeMux) {
		return nil, errors.New("link: explicit pool or mux mode required")
	}
	t := &Transport{mode: mode, closed: make(chan struct{})}
	dial := func(ctx context.Context) (vlink.PooledLink, error) {
		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		conn, err := (&tls.Dialer{Config: owner.TLS.Clone()}).DialContext(dialCtx, "tcp", owner.PeerAddr)
		if err != nil {
			return nil, err
		}
		mux, peer, err := owner.handshake(dialCtx, conn.(*tls.Conn), true)
		if err != nil {
			return nil, err
		}
		if !owner.track(mux) {
			mux.Close(context.Canceled)
			return nil, context.Canceled
		}
		return &pooledLink{mux: mux, peer: peer, owner: owner}, nil
	}
	var err error
	t.tcp, err = vlink.NewLinkPool(bounds, dial)
	if err != nil {
		return nil, err
	}
	t.udp, err = vlink.NewLinkPool(bounds, dial)
	if err != nil {
		t.tcp.Close()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			t.Close()
		case <-t.closed:
		}
	}()
	return t, nil
}

func (t *Transport) Warm(ctx context.Context) error { return t.tcp.Scale(ctx, 0, true) }
func (t *Transport) Acquire(ctx context.Context, network vlink.StreamNetwork) (*vlink.MuxLink, func(), error) {
	item, release, err := t.acquire(ctx, network)
	if err != nil {
		return nil, nil, err
	}
	return item.mux, release, nil
}

func (t *Transport) acquire(ctx context.Context, network vlink.StreamNetwork) (*pooledLink, func(), error) {
	p, mode := t.tcp, t.mode
	if network == vlink.StreamUDP {
		p, mode = t.udp, vlink.ModeMux
	}
	item, release, err := p.Acquire(ctx, mode)
	if err != nil {
		return nil, nil, err
	}
	link := item.(*pooledLink)
	slog.Info("vlink outgoing lease", "mode", mode, "network", network, "pool_total", p.Snapshot().Total)
	return link, release, nil
}
func (t *Transport) Close() {
	t.once.Do(func() { close(t.closed); _ = t.tcp.Close(); _ = t.udp.Close() })
}

func (s *Supervisor) Use(ctx context.Context, network vlink.StreamNetwork, handler LinkHandler) error {
	if s.Transport == nil {
		return s.Dial(ctx, handler)
	}
	item, release, err := s.Transport.acquire(ctx, network)
	if err != nil {
		return err
	}
	defer release()
	return handler(ctx, item.mux, item.peer)
}
