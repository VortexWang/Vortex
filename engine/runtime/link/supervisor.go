package link

import (
	"bytes"
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
	"time"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

type Supervisor struct {
	NodeID           vlink.NodeID
	Signer           crypto.Signer
	TLS              *tls.Config
	ListenAddr       string
	PeerAddr         string
	ExpectedPeer     vlink.NodeID
	BootID           vlink.BootID
	Generation       uint64
	Local            bool
	identityProvider IdentityProvider
	InboundPeer      vlink.NodeID
	mu               sync.Mutex
	links            map[*vlink.MuxLink]struct{}
	closed           bool
	Transport        *Transport
	Routes           *Routes
}

type LinkHandler func(context.Context, *vlink.MuxLink, vlink.NodeID) error

func New(cfg config.Config) (*Supervisor, error) {
	identity, err := cfg.LoadIdentity(cfg.Role != config.RoleSource)
	if err != nil {
		return nil, err
	}
	return newSupervisor(cfg, identity)
}

func newSupervisor(cfg config.Config, identity config.Identity) (*Supervisor, error) {
	expected, _ := vlink.ParseNodeID(cfg.NextHopNodeID)
	inbound, _ := vlink.ParseNodeID(cfg.PeerNodeID)
	var boot vlink.BootID
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, fmt.Errorf("link: generate boot id: %w", err)
	}
	return &Supervisor{NodeID: identity.NodeID, Signer: identity.Signer, TLS: identity.TLS, ListenAddr: cfg.Listen, PeerAddr: cfg.Peer, ExpectedPeer: expected, InboundPeer: inbound, BootID: boot, Generation: cfg.PathGeneration, Local: cfg.Role == config.RoleSource, links: make(map[*vlink.MuxLink]struct{})}, nil
}

func (s *Supervisor) hello(client bool, identity RuntimeIdentity) vlink.HelloConfig {
	return vlink.HelloConfig{Client: client, NodeID: s.NodeID, BootID: s.BootID, Generation: s.Generation, Capabilities: []uint16{1, 2}, Signer: identity.Signer, VerifyPeer: func(_ context.Context, cert *x509.Certificate, _ vlink.HelloMessage) error {
		_, err := vlink.NodeIDFromCertificate(cert)
		if err == nil && identity.Check != nil {
			err = identity.Check(cert)
		}
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
	snapshot, config, err := s.capture(false)
	if err != nil {
		_ = conn.Close()
		return
	}
	link, peer, err := s.handshake(ctx, tls.Server(conn, config), false, snapshot)
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
	link, peer, err := s.dial(ctx)
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

func (s *Supervisor) handshake(ctx context.Context, conn *tls.Conn, client bool, identity RuntimeIdentity) (*vlink.MuxLink, vlink.NodeID, error) {
	// Bound receive storage above the 2 MiB stream credit. Batched credit
	// updates keep bulk transfers within the control-frame rate budget. The
	// larger aggregate reserve absorbs UDP bursts while concurrent TCP streams
	// remain bounded by the aggregate credit ceiling.
	link, peer, err := vlink.NewAuthenticatedMuxLink(ctx, conn, s.hello(client, identity), vlink.MuxConfig{Admit: func() error {
		state := conn.ConnectionState()
		if identity.Check != nil {
			if len(state.PeerCertificates) == 0 {
				return errors.New("link: peer identity missing")
			}
			return identity.Check(state.PeerCertificates[0])
		}
		return nil
	}, LocalClient: client, MaxPendingOpenBytes: 64 * 1024, StreamWindow: 8 * 1024 * 1024, AggregateWindow: 64 * 1024 * 1024, MaxWindow: 128 * 1024 * 1024, UDPWindowReserve: 16 * 1024 * 1024, StreamQueueFrames: 512, UDPQueueFrames: 8192, UDPQueueBytes: 16 << 20})
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
	if s.links == nil {
		s.links = make(map[*vlink.MuxLink]struct{})
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

// RuntimeIdentity is a per-handshake snapshot. The provider must never mutate its
// certificate/key material. Check consults current revocation state for both the
// captured local identity and peer; rotation alone does not invalidate a link.
type RuntimeIdentity struct {
	NodeID      vlink.NodeID
	Certificate tls.Certificate
	Signer      crypto.Signer
	Check       func(*x509.Certificate) error
}
type IdentityProvider func() (RuntimeIdentity, error)

// SetIdentityProvider is configured before listeners/pools start.
func (s *Supervisor) SetIdentityProvider(provider IdentityProvider, boot vlink.BootID) error {
	if provider == nil || boot == (vlink.BootID{}) {
		return errors.New("link: identity provider and boot required")
	}
	id, err := provider()
	if err != nil {
		return err
	}
	if id.NodeID != s.NodeID {
		return errors.New("link: provider node identity mismatch")
	}
	s.identityProvider, s.BootID = provider, boot
	return nil
}

func (s *Supervisor) capture(client bool) (RuntimeIdentity, *tls.Config, error) {
	config := s.TLS.Clone()
	config.SessionTicketsDisabled, config.ClientSessionCache = true, nil
	id := RuntimeIdentity{NodeID: s.NodeID, Signer: s.Signer}
	if s.identityProvider != nil {
		var err error
		id, err = s.identityProvider()
		if err != nil {
			return id, nil, err
		}
		if id.NodeID != s.NodeID || id.Signer == nil || len(id.Certificate.Certificate) == 0 {
			return id, nil, errors.New("link: invalid runtime identity")
		}
		leaf, err := x509.ParseCertificate(id.Certificate.Certificate[0])
		if err != nil {
			return id, nil, err
		}
		pub, err := x509.MarshalPKIXPublicKey(id.Signer.Public())
		if err != nil {
			return id, nil, err
		}
		certPub, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
		if err != nil || !bytes.Equal(pub, certPub) {
			return id, nil, errors.New("link: HELLO signer mismatch")
		}
		tlsSigner, ok := id.Certificate.PrivateKey.(crypto.Signer)
		if !ok {
			return id, nil, errors.New("link: missing TLS signer")
		}
		tlsPub, err := x509.MarshalPKIXPublicKey(tlsSigner.Public())
		if err != nil || !bytes.Equal(pub, tlsPub) {
			return id, nil, errors.New("link: TLS signer mismatch")
		}
		config.Certificates = []tls.Certificate{id.Certificate}
		config.GetCertificate, config.GetClientCertificate, config.GetConfigForClient = nil, nil, nil
		if client {
			config.ServerName = s.ExpectedPeer.String()
		}
	} else if len(config.Certificates) > 0 {
		id.Certificate = config.Certificates[0]
	}
	if !client {
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	previous := config.VerifyConnection
	config.VerifyConnection = func(state tls.ConnectionState) error {
		if previous != nil {
			if err := previous(state); err != nil {
				return err
			}
		}
		if id.Check != nil {
			if len(state.PeerCertificates) > 0 && !client && s.InboundPeer != (vlink.NodeID{}) {
				peer, err := vlink.NodeIDFromCertificate(state.PeerCertificates[0])
				if err != nil || peer != s.InboundPeer {
					return errors.New("link: unexpected upstream identity")
				}
			}
			if len(state.PeerCertificates) == 0 {
				return errors.New("link: peer missing")
			}
			return id.Check(state.PeerCertificates[0])
		}
		// Static qualification identities still have no expiry grace.
		if len(id.Certificate.Certificate) > 0 {
			leaf, err := x509.ParseCertificate(id.Certificate.Certificate[0])
			if err != nil {
				return err
			}
			now := time.Now()
			if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
				return errors.New("link: local certificate expired")
			}
		}
		return nil
	}
	return id, config, nil
}

func (s *Supervisor) dial(ctx context.Context) (*vlink.MuxLink, vlink.NodeID, error) {
	id, config, err := s.capture(true)
	if err != nil {
		return nil, vlink.NodeID{}, err
	}
	conn, err := (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", s.PeerAddr)
	if err != nil {
		return nil, vlink.NodeID{}, fmt.Errorf("link: dial %s: %w", s.PeerAddr, err)
	}
	mux, peer, err := s.handshake(ctx, conn.(*tls.Conn), true, id)
	if err != nil {
		_ = conn.Close()
	}
	return mux, peer, err
}

// NewManaged defers certificate selection to the Agent. It reads only pinned
// runtime trust, never stale certificate/key compatibility aliases.
func NewManaged(cfg config.Config) (*Supervisor, error) {
	id, err := vlink.ParseNodeID(cfg.NodeID)
	if err != nil {
		return nil, err
	}
	configTLS, err := cfg.TLSConfigWithCertificate(true, tls.Certificate{})
	if err != nil {
		return nil, err
	}
	return newSupervisor(cfg, config.Identity{NodeID: id, TLS: configTLS})
}

func (s *Supervisor) HasIdentityProvider() bool { return s.identityProvider != nil }
