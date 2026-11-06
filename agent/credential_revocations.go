package agent

import (
	"context"
	"crypto/x509"
	"errors"
	"sync"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// CredentialRevocationView is an Agent-owned immutable publication of FULL
// committed additive revocations. Engine callbacks only read this in-memory
// view. No TLS handshake or OPEN opens a database or contacts a control plane.
type CredentialRevocationView struct {
	mu             sync.RWMutex
	db             *store.Store
	serials, nodes map[string]time.Time
	clock          func() (time.Time, time.Time, bool)
}

func LoadCredentialRevocations(ctx context.Context, db *store.Store, clock func() (time.Time, time.Time, bool)) (*CredentialRevocationView, error) {
	if db == nil || clock == nil {
		return nil, errors.New("agent: durable revocation store and clock required")
	}
	batches, err := db.CredentialRevocations(ctx)
	if err != nil {
		return nil, err
	}
	v := &CredentialRevocationView{db: db, clock: clock, serials: map[string]time.Time{}, nodes: map[string]time.Time{}}
	for _, r := range batches {
		v.install(r)
	}
	return v, nil
}
func (v *CredentialRevocationView) install(r *controlv1.CredentialRevocations) {
	at, _ := time.Parse(time.RFC3339Nano, r.EffectiveAt)
	for _, set := range []struct {
		keys   []string
		target map[string]time.Time
	}{{r.CertificateSerials, v.serials}, {r.NodeIds, v.nodes}} {
		for _, key := range set.keys {
			old, ok := set.target[key]
			if !ok || at.Before(old) {
				set.target[key] = at
			}
		}
	}
}
func (v *CredentialRevocationView) Apply(ctx context.Context, issuer string, r *controlv1.CredentialRevocations) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.db.PersistCredentialRevocations(ctx, issuer, r); err != nil {
		return err
	}
	v.install(r)
	return nil
}
func (v *CredentialRevocationView) IsRevoked(cert *x509.Certificate) bool {
	if cert == nil {
		return true
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, latest, trusted := v.clock()
	blocked := func(at time.Time, exists bool) bool { return exists && (!trusted || !latest.Before(at)) }
	at, ok := v.serials[cert.SerialNumber.String()]
	if blocked(at, ok) {
		return true
	}
	for _, id := range cert.DNSNames {
		at, ok = v.nodes[id]
		if blocked(at, ok) {
			return true
		}
	}
	return false
}
