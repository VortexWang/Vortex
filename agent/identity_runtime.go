package agent

import (
	"crypto/x509"
	"errors"
	"time"

	"vortex/agent/identity"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

// RuntimeIdentity captures a single durably selected identity under the same
// manager lock used by renewal publication. Its key/leaf are immutable; Check
// queries the current revocation view, including for already-established links.
func (m *CredentialManager) RuntimeIdentity() (link.RuntimeIdentity, error) {
	if m == nil {
		return link.RuntimeIdentity{}, errors.New("agent: credential manager required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.set.Active
	if err := identity.ValidateRuntimeCredential(c, time.Now()); err != nil {
		return link.RuntimeIdentity{}, err
	}
	if _, err := c.Leaf.Verify(x509.VerifyOptions{Roots: m.roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}); err != nil {
		return link.RuntimeIdentity{}, err
	}
	id, _ := vlink.ParseNodeID(c.NodeID)
	check := func(peer *x509.Certificate) error {
		now := time.Now()
		if peer == nil {
			return errors.New("agent: runtime peer certificate missing")
		}
		for _, leaf := range []*x509.Certificate{c.Leaf, peer} {
			if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) || (m.revoked != nil && m.revoked(leaf)) {
				return errors.New("agent: runtime certificate expired or revoked")
			}
		}
		peerID, err := vlink.NodeIDFromCertificate(peer)
		if err != nil {
			return err
		}
		if len(peer.DNSNames) != 1 || peer.DNSNames[0] != peerID.String() || len(peer.URIs) != 1 {
			return errors.New("agent: runtime peer SAN mismatch")
		}
		if peer.KeyUsage != x509.KeyUsageDigitalSignature || len(peer.ExtKeyUsage) != 2 || len(peer.UnknownExtKeyUsage) != 0 {
			return errors.New("agent: runtime peer usage mismatch")
		}
		client, server := false, false
		for _, usage := range peer.ExtKeyUsage {
			client = client || usage == x509.ExtKeyUsageClientAuth
			server = server || usage == x509.ExtKeyUsageServerAuth
		}
		if !client || !server {
			return errors.New("agent: runtime peer EKU mismatch")
		}
		return nil
	}
	if m.revoked != nil && m.revoked(c.Leaf) {
		return link.RuntimeIdentity{}, errors.New("agent: local runtime identity revoked")
	}
	return link.RuntimeIdentity{NodeID: id, Certificate: c.Certificate, Signer: c.PrivateKey, Check: check}, nil
}

func (e *RuntimeEngine) SetIdentityProvider(manager *CredentialManager, bootID string) error {
	if e == nil || e.runtime == nil || manager == nil {
		return errors.New("agent: runtime and credentials required")
	}
	id, err := vlink.ParseNodeID(bootID)
	if err != nil {
		return err
	}
	e.credentials = manager
	return e.runtime.SetIdentityProvider(manager.RuntimeIdentity, vlink.BootID(id))
}
