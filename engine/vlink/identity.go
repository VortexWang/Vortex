package vlink

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrStalePeer          = errors.New("vlink: stale peer generation or boot")
	ErrHelloReplay        = errors.New("vlink: repeated HELLO nonce")
	ErrLeaseRTT           = errors.New("vlink: lease response RTT exceeds limit")
	ErrLeaseReplay        = errors.New("vlink: duplicate lease request, nonce, or revision")
	ErrLeaseClockRollback = errors.New("vlink: lease request time moved backwards")
	ErrLeaseExpired       = errors.New("vlink: conservative lease deadline has elapsed")
	ErrFreshnessCapacity  = errors.New("vlink: peer freshness replay cache is full")
	ErrLeaseCapacity      = errors.New("vlink: lease replay cache is full")
)

type PeerPolicy func(context.Context, *x509.Certificate, NodeID) error

type PeerCertificatePolicy struct{ Authorize PeerPolicy }

func (p PeerCertificatePolicy) Verify(ctx context.Context, certificates []*x509.Certificate, verifiedChains [][]*x509.Certificate) error {
	if len(certificates) == 0 || len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 || certificates[0] == nil || !bytes.Equal(certificates[0].Raw, verifiedChains[0][0].Raw) {
		return fmt.Errorf("%w: verified peer certificate chain is required", ErrProtocol)
	}
	id, err := NodeIDFromCertificate(certificates[0])
	if err != nil {
		return err
	}
	if p.Authorize == nil {
		return errors.New("vlink: peer certificate policy callback is required")
	}
	if err := p.Authorize(ctx, certificates[0], id); err != nil {
		return fmt.Errorf("%w: peer certificate policy rejected identity", ErrProtocol)
	}
	return nil
}

// TLS13Config preserves standard chain verification and adds VLink node SAN,
// revocation, and current-path policy checks after the handshake.
func TLS13Config(config *tls.Config, policy PeerCertificatePolicy, server bool) (*tls.Config, error) {
	if config == nil || policy.Authorize == nil {
		return nil, errors.New("vlink: TLS config and peer policy are required")
	}
	clone := config.Clone()
	clone.SessionTicketsDisabled, clone.ClientSessionCache = true, nil
	clone.MinVersion, clone.MaxVersion = tls.VersionTLS13, tls.VersionTLS13
	if server {
		clone.ClientAuth = tls.RequireAndVerifyClientCert
	}
	previous := clone.VerifyConnection
	clone.VerifyConnection = func(state tls.ConnectionState) error {
		if state.Version != tls.VersionTLS13 || !state.HandshakeComplete {
			return fmt.Errorf("%w: TLS 1.3 is required", ErrProtocol)
		}
		if previous != nil {
			if err := previous(state); err != nil {
				return err
			}
		}
		if err := policy.Verify(context.Background(), state.PeerCertificates, state.VerifiedChains); err != nil {
			return err
		}
		return nil
	}
	return clone, nil
}

type peerFreshness struct {
	boot       BootID
	generation uint64
	observed   time.Time
	retired    map[BootID]struct{}
	nonces     map[HelloNonce]struct{}
}
type PeerFreshness struct {
	mu        sync.Mutex
	maxNonces int
	peers     map[NodeID]*peerFreshness
}

func NewPeerFreshness(maxNoncesPerPeer int) *PeerFreshness {
	if maxNoncesPerPeer < 1 {
		maxNoncesPerPeer = 1
	}
	return &PeerFreshness{maxNonces: maxNoncesPerPeer, peers: make(map[NodeID]*peerFreshness)}
}

func (f *PeerFreshness) Accept(peer NodeID, boot BootID, generation uint64, nonce HelloNonce, now time.Time) error {
	if peer == (NodeID{}) || boot == (BootID{}) || generation == 0 || nonce == (HelloNonce{}) {
		return fmt.Errorf("%w: incomplete peer freshness tuple", ErrProtocol)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.peers[peer]
	if state == nil {
		state = &peerFreshness{boot: boot, retired: make(map[BootID]struct{}), nonces: make(map[HelloNonce]struct{})}
		f.peers[peer] = state
	}
	if _, retired := state.retired[boot]; retired {
		return ErrStalePeer
	}
	if !state.observed.IsZero() && now.Before(state.observed) {
		return ErrStalePeer
	}
	if _, replay := state.nonces[nonce]; replay {
		return ErrHelloReplay
	}
	if len(state.nonces) >= f.maxNonces {
		return ErrFreshnessCapacity
	}
	if state.boot != boot {
		state.retired[state.boot] = struct{}{}
		state.boot, state.generation = boot, generation
	} else if generation < state.generation {
		return ErrStalePeer
	} else if generation > state.generation {
		state.generation = generation
	}
	state.observed = now
	state.nonces[nonce] = struct{}{}
	return nil
}

type LeaseGrant struct {
	RequestID        [16]byte
	Nonce            HelloNonce
	ConnectionID     [16]byte
	BootID           BootID
	Generation       uint64
	Revision         uint64
	TTL              time.Duration
	RequestStart     time.Time
	ResponseAt       time.Time
	VersionDeadline  time.Time
	RuntimeDeadline  time.Time
	ShareDeadline    time.Time
	AutonomyDeadline time.Time
}

type leaseConnection struct {
	boot                 BootID
	generation, revision uint64
	requestStart         time.Time
}
type LeaseTracker struct {
	mu          sync.Mutex
	requests    map[[16]byte]struct{}
	nonces      map[HelloNonce]struct{}
	connections map[[16]byte]leaseConnection
	maxEntries  int
}

func NewLeaseTracker() *LeaseTracker {
	return &LeaseTracker{requests: make(map[[16]byte]struct{}), nonces: make(map[HelloNonce]struct{}), connections: make(map[[16]byte]leaseConnection), maxEntries: 4096}
}

func (t *LeaseTracker) Accept(grant LeaseGrant) (time.Time, error) {
	if grant.RequestID == ([16]byte{}) || grant.Nonce == (HelloNonce{}) || grant.ConnectionID == ([16]byte{}) || grant.BootID == (BootID{}) || grant.Generation == 0 || grant.Revision == 0 || grant.TTL <= 0 || grant.RequestStart.IsZero() || grant.ResponseAt.IsZero() {
		return time.Time{}, fmt.Errorf("%w: invalid lease grant", ErrProtocol)
	}
	rtt := grant.ResponseAt.Sub(grant.RequestStart)
	if rtt < 0 {
		return time.Time{}, ErrLeaseClockRollback
	}
	if rtt > 10*time.Second {
		return time.Time{}, ErrLeaseRTT
	}
	grace := grant.TTL / 10
	if grace < 10*time.Second {
		grace = 10 * time.Second
	}
	if grant.TTL <= grace {
		return time.Time{}, fmt.Errorf("%w: lease TTL is not greater than its safety margin", ErrProtocol)
	}
	deadline := grant.RequestStart.Add(grant.TTL - grace)
	for _, bound := range []time.Time{grant.VersionDeadline, grant.RuntimeDeadline, grant.ShareDeadline, grant.AutonomyDeadline} {
		if !bound.IsZero() && bound.Before(deadline) {
			deadline = bound
		}
	}
	if !deadline.After(grant.ResponseAt) {
		return time.Time{}, ErrLeaseExpired
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.requests[grant.RequestID]; exists {
		return time.Time{}, ErrLeaseReplay
	}
	if _, exists := t.nonces[grant.Nonce]; exists {
		return time.Time{}, ErrLeaseReplay
	}
	if len(t.requests) >= t.maxEntries || len(t.connections) >= t.maxEntries {
		return time.Time{}, ErrLeaseCapacity
	}
	previous, exists := t.connections[grant.ConnectionID]
	if exists {
		if previous.boot != grant.BootID || previous.generation != grant.Generation || grant.Revision <= previous.revision {
			return time.Time{}, ErrLeaseReplay
		}
		if grant.RequestStart.Before(previous.requestStart) {
			return time.Time{}, ErrLeaseClockRollback
		}
	}
	t.requests[grant.RequestID] = struct{}{}
	t.nonces[grant.Nonce] = struct{}{}
	t.connections[grant.ConnectionID] = leaseConnection{boot: grant.BootID, generation: grant.Generation, revision: grant.Revision, requestStart: grant.RequestStart}
	return deadline, nil
}
