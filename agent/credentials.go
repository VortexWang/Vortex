package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
)

type CredentialManager struct {
	mu         sync.RWMutex
	set        identity.CredentialSet
	roots      *x509.CertPool
	serverName string
	revoked    func(*x509.Certificate) bool
}

func NewCredentialManager(initial identity.Credential, roots *x509.CertPool, serverName string, revoked func(*x509.Certificate) bool) (*CredentialManager, error) {
	if initial.Leaf == nil || initial.Certificate.PrivateKey == nil || roots == nil || serverName == "" {
		return nil, errors.New("agent credentials: initial identity, trust roots and server name are required")
	}
	return &CredentialManager{set: identity.CredentialSet{Active: initial}, roots: roots.Clone(), serverName: serverName, revoked: revoked}, nil
}

func LoadCredentialManager(ctx context.Context, db *store.Store, nodeID, directory, serverName string, roots *x509.CertPool, revoked func(*x509.Certificate) bool, now time.Time) (*CredentialManager, error) {
	if db == nil || nodeID == "" || directory == "" || roots == nil || serverName == "" {
		return nil, errors.New("agent credentials: persisted identity, directory, roots and server name are required")
	}
	versions, err := db.CredentialVersionRecords(ctx, nodeID, now)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, errors.New("agent credentials: no currently valid persisted credential")
	}
	activeDER, _, err := db.Credential(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("agent credentials: load durable active credential: %w", err)
	}
	set := identity.CredentialSet{}
	keepSerials := make(map[string]struct{}, len(versions))
	activeLoaded := false
	for _, version := range versions {
		leaf, err := x509.ParseCertificate(version.Certificate)
		if err != nil {
			return nil, err
		}
		isActive := string(version.Certificate) == string(activeDER)
		if revoked != nil && revoked(leaf) {
			if isActive {
				_ = db.RevokeCredential(ctx, nodeID)
				return nil, errors.New("agent credentials: active persisted credential is revoked")
			}
			if err := db.RevokeCredentialVersion(ctx, nodeID, leaf.SerialNumber.String()); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			return nil, fmt.Errorf("agent credentials: persisted certificate verification: %w", err)
		}
		serial := leaf.SerialNumber.String()
		keepSerials[serial] = struct{}{}
		keyPair, err := tls.LoadX509KeyPair(directory+"/runtime-cert-"+serial+".pem", directory+"/runtime-key-"+serial+".pem")
		if err != nil {
			return nil, err
		}
		keyLeaf, err := x509.ParseCertificate(keyPair.Certificate[0])
		if err != nil {
			return nil, err
		}
		if string(keyLeaf.Raw) != string(version.Certificate) {
			return nil, errors.New("agent credentials: persisted key pair differs from certificate")
		}
		private, ok := keyPair.PrivateKey.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("agent credentials: persisted private key type is unsupported")
		}
		credential, err := identity.CredentialFromTLS(nodeID, keyPair, leaf, private)
		if err != nil {
			return nil, err
		}
		if isActive {
			set.Active = credential
			activeLoaded = true
		} else if set.Previous == nil {
			previous := credential
			set.Previous = &previous
			set.PreviousUntil = version.OverlapUntil
		}
		keepSerials[serial] = struct{}{}
	}
	if !activeLoaded || set.Active.Leaf == nil {
		return nil, errors.New("agent credentials: persisted active credential is unavailable")
	}
	manager, err := NewCredentialManager(set.Active, roots, serverName, revoked)
	if err != nil {
		return nil, err
	}
	manager.set = set
	if err := identity.PruneVersionFiles(directory, keepSerials); err != nil {
		return nil, fmt.Errorf("agent credentials: prune expired credential files: %w", err)
	}
	return manager, nil
}

func (m *CredentialManager) TLSConfig() (*tls.Config, error) {
	if m == nil {
		return nil, errors.New("agent credentials: manager is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	config, err := identity.TLSConfigSet(m.set, m.roots, m.revoked, false, time.Now())
	if err != nil {
		return nil, err
	}
	config.ServerName = m.serverName
	return config, nil
}

func (m *CredentialManager) Snapshot() identity.CredentialSet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.set
}

func (m *CredentialManager) Renew(ctx context.Context, address, directory string, db *store.Store, overlap time.Duration) error {
	if m == nil || db == nil || address == "" || directory == "" || overlap <= 0 {
		return errors.New("agent credentials: renewal dependencies and positive overlap are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revoked != nil && m.revoked(m.set.Active.Leaf) {
		return errors.New("agent credentials: active identity revoked")
	}
	next, caDER, err := exchangeCredentialWithRevocations(ctx, m.set.Active.NodeID, "", address, m.roots, m.serverName, &m.set.Active, m.revoked)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if now.Before(m.set.Active.Leaf.NotBefore) || !now.Before(m.set.Active.Leaf.NotAfter) || (m.revoked != nil && m.revoked(m.set.Active.Leaf)) {
		return errors.New("agent credentials: renewal authority expired or revoked during exchange")
	}
	if len(m.set.Active.Leaf.URIs) > 0 {
		if err := identity.ValidateRuntimeCredential(next, now); err != nil {
			return err
		}
	}
	nextSet, err := m.set.Rotate(next, now, overlap, func(candidate identity.Credential) error {
		if m.revoked != nil && m.revoked(candidate.Leaf) {
			return errors.New("replacement credential is revoked")
		}
		_, err := candidate.Leaf.Verify(x509.VerifyOptions{Roots: m.roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
		return err
	})
	if err != nil {
		return fmt.Errorf("agent credentials: validate replacement: %w", err)
	}
	if err := identity.SaveVersionFiles(directory, next); err != nil {
		return fmt.Errorf("agent credentials: persist replacement files: %w", err)
	}
	if err := db.SaveCredentialRotation(ctx, next.NodeID, next.Leaf.Raw, next.Leaf.NotAfter, m.set.Active.Leaf.Raw, m.set.Active.Leaf.NotAfter, now.Add(overlap)); err != nil {
		return fmt.Errorf("agent credentials: persist replacement: %w", err)
	}
	m.set = nextSet
	if err := identity.SaveActiveFiles(directory, next, caDER); err != nil {
		return fmt.Errorf("agent credentials: update active credential aliases: %w", err)
	}
	keepSerials := map[string]struct{}{next.Leaf.SerialNumber.String(): {}}
	if nextSet.Previous != nil && time.Now().Before(nextSet.PreviousUntil) {
		keepSerials[nextSet.Previous.Leaf.SerialNumber.String()] = struct{}{}
	}
	if err := identity.PruneVersionFiles(directory, keepSerials); err != nil {
		return fmt.Errorf("agent credentials: prune expired credential files: %w", err)
	}
	return nil
}

func (m *CredentialManager) RunRenewal(ctx context.Context, address, directory string, db *store.Store, renewBefore, overlap, retry time.Duration) error {
	if renewBefore <= 0 || overlap <= 0 || retry <= 0 {
		return errors.New("agent credentials: positive renew-before, overlap and retry intervals are required")
	}
	for {
		m.mu.RLock()
		active := m.set.Active
		m.mu.RUnlock()
		wait := time.Until(active.Leaf.NotAfter.Add(-renewBefore))
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		for {
			if err := m.Renew(ctx, address, directory, db, overlap); err == nil {
				break
			} else if !time.Now().Before(active.Leaf.NotAfter) {
				return fmt.Errorf("agent credentials: renewal failed after expiry: %w", err)
			}
			timer.Reset(retry)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
