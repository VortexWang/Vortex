package link

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

type Supervisor struct {
	NodeID       vlink.NodeID
	Signer       crypto.Signer
	TLS          *tls.Config
	ListenAddr   string
	PeerAddr     string
	ExpectedPeer vlink.NodeID
	BootID       vlink.BootID
	Generation   uint64
	Local        bool
	mu           sync.Mutex
	links        map[*vlink.MuxLink]struct{}
	closed       bool
	Transport    *Transport
	Routes       *Routes
}

type LinkHandler func(context.Context, *vlink.MuxLink, vlink.NodeID) error

func New(cfg config.Config) (*Supervisor, error) {
	identity, err := cfg.LoadIdentity(cfg.Role != config.RoleSource)
	if err != nil {
		return nil, err
	}
	expected, _ := vlink.ParseNodeID(cfg.NextHopNodeID)
	var boot vlink.BootID
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, fmt.Errorf("link: generate boot id: %w", err)
	}
	return &Supervisor{NodeID: identity.NodeID, Signer: identity.Signer, TLS: identity.TLS, ListenAddr: cfg.Listen, PeerAddr: cfg.Peer, ExpectedPeer: expected, BootID: boot, Generation: cfg.PathGeneration, Local: cfg.Role == config.RoleSource, links: make(map[*vlink.MuxLink]struct{})}, nil
}

func (s *Supervisor) hello(client bool) vlink.HelloConfig {
	return vlink.HelloConfig{Client: client, NodeID: s.NodeID, BootID: s.BootID, Generation: s.Generation, Capabilities: []uint16{1, 2}, Signer: s.Signer, VerifyPeer: func(_ context.Context, cert *x509.Certificate, _ vlink.HelloMessage) error {
		_, err := vlink.NodeIDFromCertificate(cert)
		return err
	}}
}

func (s *Supervisor) Serve(ctx context.Context, handler LinkHandler) error {
	listener, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return fmt.Errorf("link: listen %s: %w", s.ListenAddr, err)
	}
	return s.ServeListener(ctx, listener, handler)
}

// ServeListener takes ownership of an already bound listener.
func (s *Supervisor) ServeListener(ctx context.Context, listener net.Listener, handler LinkHandler) error {
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			s.Close()
		case <-done:
		}
	}()
	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(acceptErr, net.ErrClosed) {
				if e, ok := acceptErr.(net.Error); ok && e.Temporary() {
					continue
				}
			}
			return acceptErr
		}
		go s.accept(ctx, conn, handler)
	}
}

func (s *Supervisor) accept(ctx context.Context, conn net.Conn, handler LinkHandler) {
	link, peer, err := s.handshake(ctx, tls.Server(conn, s.TLS.Clone()), false)
	if err != nil {
		slog.Error("vlink inbound handshake failed", "err", err)
		_ = conn.Close()
		return
	}
	if !s.track(link) {
		link.Close(context.Canceled)
		return
	}
	defer s.untrack(link)
	if handler != nil {
		if err := handler(ctx, link, peer); err != nil && ctx.Err() == nil {
			slog.Error("vlink inbound handler failed", "peer", peer.String(), "err", err)
		}
	}
}

func (s *Supervisor) Dial(ctx context.Context, handler LinkHandler) error {
	dialer := tls.Dialer{Config: s.TLS.Clone()}
	conn, err := dialer.DialContext(ctx, "tcp", s.PeerAddr)
	if err != nil {
		return fmt.Errorf("link: dial %s: %w", s.PeerAddr, err)
	}
	link, peer, err := s.handshake(ctx, conn.(*tls.Conn), true)
	if err != nil {
		return err
	}
	if !s.track(link) {
		link.Close(context.Canceled)
		return context.Canceled
	}
	defer s.untrack(link)
	if handler != nil {
		return handler(ctx, link, peer)
	}
	return nil
}

func (s *Supervisor) handshake(ctx context.Context, conn *tls.Conn, client bool) (*vlink.MuxLink, vlink.NodeID, error) {
	// Bound receive storage above the 2 MiB stream credit. Batched credit
	// updates keep bulk transfers within the control-frame rate budget. The
	// larger aggregate reserve absorbs UDP bursts while concurrent TCP streams
	// remain bounded by the aggregate credit ceiling.
	link, peer, err := vlink.NewAuthenticatedMuxLink(ctx, conn, s.hello(client), vlink.MuxConfig{LocalClient: client, MaxPendingOpenBytes: 64 * 1024, StreamWindow: 8 * 1024 * 1024, AggregateWindow: 64 * 1024 * 1024, MaxWindow: 128 * 1024 * 1024, UDPWindowReserve: 16 * 1024 * 1024, StreamQueueFrames: 512, UDPQueueFrames: 8192, UDPQueueBytes: 16 << 20})
	if err != nil {
		return nil, vlink.NodeID{}, err
	}
	if client && s.ExpectedPeer != (vlink.NodeID{}) && peer != s.ExpectedPeer {
		link.Close(vlink.ErrProtocol)
		return nil, vlink.NodeID{}, errors.New("link: unexpected authenticated next-hop identity")
	}
	state := conn.ConnectionState()
	slog.Info("vlink TLS negotiated", "version", state.Version, "cipher", tls.CipherSuiteName(state.CipherSuite))
	return link, peer, nil
}

func (s *Supervisor) track(link *vlink.MuxLink) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.links[link] = struct{}{}
	return true
}

func (s *Supervisor) untrack(link *vlink.MuxLink) {
	link.Close(nil)
	s.mu.Lock()
	delete(s.links, link)
	s.mu.Unlock()
}

func (s *Supervisor) Close() {
	s.mu.Lock()
	s.closed = true
	links := make([]*vlink.MuxLink, 0, len(s.links))
	for link := range s.links {
		links = append(links, link)
	}
	s.mu.Unlock()
	for _, link := range links {
		link.Close(errors.New("link: supervisor closed"))
	}
}
