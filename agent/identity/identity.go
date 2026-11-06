package identity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"vortex/engine/vlink"
)

type Credential struct {
	NodeID      string
	PrivateKey  *ecdsa.PrivateKey
	Certificate tls.Certificate
	Leaf        *x509.Certificate
}

func CredentialFromTLS(nodeID string, certificate tls.Certificate, leaf *x509.Certificate, key *ecdsa.PrivateKey) (Credential, error) {
	if nodeID == "" || leaf == nil || key == nil || len(certificate.Certificate) == 0 || !bytes.Equal(certificate.Certificate[0], leaf.Raw) {
		return Credential{}, errors.New("identity: incomplete TLS credential")
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != nodeID || leaf.Subject.CommonName != nodeID {
		return Credential{}, errors.New("identity: TLS credential node SAN mismatch")
	}
	if !key.PublicKey.Equal(leaf.PublicKey) {
		return Credential{}, errors.New("identity: TLS signer mismatch")
	}
	actual, ok := certificate.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || !actual.PublicKey.Equal(&key.PublicKey) {
		return Credential{}, errors.New("identity: TLS private key mismatch")
	}
	certificate.Leaf = leaf
	return Credential{NodeID: nodeID, PrivateKey: key, Certificate: certificate, Leaf: leaf}, nil
}

type CA struct {
	Certificate *x509.Certificate
	Key         *ecdsa.PrivateKey
}

type Enroller struct {
	CA     *CA
	Tokens map[string]bool
	mu     sync.Mutex
}

type CredentialSet struct {
	Active        Credential
	Previous      *Credential
	PreviousUntil time.Time
}

// Rotate validates the replacement before making it active and retaining the prior credential for overlap.
func (s CredentialSet) Rotate(next Credential, now time.Time, overlap time.Duration, validate func(Credential) error) (CredentialSet, error) {
	if s.Active.Leaf == nil || next.Leaf == nil || s.Active.NodeID == "" || next.NodeID != s.Active.NodeID {
		return s, errors.New("identity: replacement credential identity mismatch")
	}
	if overlap <= 0 || validate == nil {
		return s, errors.New("identity: positive overlap and validation callback required")
	}
	if err := validate(next); err != nil {
		return s, fmt.Errorf("identity: replacement validation failed: %w", err)
	}
	if now.Before(next.Leaf.NotBefore) || !now.Before(next.Leaf.NotAfter) {
		return s, errors.New("identity: replacement credential is outside validity interval")
	}
	old := s.Active
	return CredentialSet{Active: next, Previous: &old, PreviousUntil: now.Add(overlap)}, nil
}

func (s CredentialSet) ValidCredentials(now time.Time) []Credential {
	credentials := []Credential{s.Active}
	if s.Previous != nil && now.Before(s.PreviousUntil) && now.Before(s.Previous.Leaf.NotAfter) {
		credentials = append(credentials, *s.Previous)
	}
	return credentials
}

func (e *Enroller) Redeem(token, nodeID string, csrDER []byte, now time.Time, ttl time.Duration) (Credential, error) {
	if e == nil || e.CA == nil || token == "" {
		return Credential{}, errors.New("identity: invalid or consumed enrollment token")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.Tokens[token] {
		return Credential{}, errors.New("identity: invalid or consumed enrollment token")
	}
	cred, err := e.CA.SignCSR(csrDER, nodeID, now, ttl)
	if err != nil {
		return Credential{}, err
	}
	delete(e.Tokens, token)
	return cred, nil
}

func NewCA(now time.Time) (*CA, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "VORTEX test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return &CA{Certificate: cert, Key: key}, der, nil
}

func GenerateCSR(nodeID string) (*ecdsa.PrivateKey, []byte, error) {
	if strings.TrimSpace(nodeID) == "" {
		return nil, nil, errors.New("identity: node_id required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID}, DNSNames: []string{nodeID}}
	if id, err := vlink.ParseNodeID(nodeID); err == nil && id.String() == nodeID {
		uri, _ := url.Parse("urn:vortex:node:" + nodeID)
		template.URIs = []*url.URL{uri}
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, nil, err
	}
	return key, csr, nil
}

func (ca *CA) SignCSR(csrDER []byte, nodeID string, now time.Time, ttl time.Duration) (Credential, error) {
	if ca == nil || ca.Certificate == nil || ca.Key == nil || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(nodeID) != nodeID || ttl <= 0 {
		return Credential{}, errors.New("identity: CA, canonical node_id and positive certificate lifetime are required")
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return Credential{}, fmt.Errorf("identity: parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return Credential{}, fmt.Errorf("identity: CSR signature: %w", err)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != nodeID || csr.Subject.CommonName != nodeID {
		return Credential{}, errors.New("identity: CSR node_id mismatch")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return Credential{}, err
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: nodeID}, DNSNames: []string{nodeID}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(ttl), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	if id, err := vlink.ParseNodeID(nodeID); err == nil && id.String() == nodeID {
		if len(csr.URIs) != 1 || csr.URIs[0].String() != "urn:vortex:node:"+nodeID {
			return Credential{}, errors.New("identity: CSR node URI mismatch")
		}
		tmpl.URIs = csr.URIs
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Certificate, csr.PublicKey, ca.Key)
	if err != nil {
		return Credential{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return Credential{}, err
	}
	return Credential{NodeID: nodeID, Certificate: tls.Certificate{Certificate: [][]byte{der}}, Leaf: leaf}, nil
}

func AttachKey(c Credential, key *ecdsa.PrivateKey, caDER []byte) (Credential, error) {
	if key == nil || c.Leaf == nil {
		return Credential{}, errors.New("identity: key and leaf required")
	}
	certKey, err := x509.MarshalPKIXPublicKey(c.Leaf.PublicKey)
	if err != nil {
		return Credential{}, err
	}
	localKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return Credential{}, err
	}
	if !bytes.Equal(certKey, localKey) {
		return Credential{}, errors.New("identity: private key does not match certificate")
	}
	if len(caDER) > 0 {
		issuer, err := x509.ParseCertificate(caDER)
		if err != nil {
			return Credential{}, fmt.Errorf("identity: parse issuer certificate: %w", err)
		}
		if err := c.Leaf.CheckSignatureFrom(issuer); err != nil {
			return Credential{}, fmt.Errorf("identity: issuer certificate mismatch: %w", err)
		}
	}
	c.PrivateKey = key
	c.Certificate.PrivateKey = key
	c.Certificate.Certificate = [][]byte{c.Leaf.Raw}
	c.Certificate.Leaf = c.Leaf
	return c, nil
}

func SaveFiles(directory string, credential Credential, caDER []byte) error {
	if err := SaveVersionFiles(directory, credential); err != nil {
		return err
	}
	return SaveActiveFiles(directory, credential, caDER)
}

func SaveVersionFiles(directory string, credential Credential) error {
	if credential.Leaf == nil || credential.Certificate.PrivateKey == nil {
		return errors.New("identity: incomplete credential")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(credential.Certificate.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: credential.Leaf.Raw})
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{{"runtime-key-" + credential.Leaf.SerialNumber.String() + ".pem", keyPEM, 0600}, {"runtime-cert-" + credential.Leaf.SerialNumber.String() + ".pem", certPEM, 0644}}
	for _, file := range files {
		if err := atomicCredentialFile(directory, file.name, file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func SaveActiveFiles(directory string, credential Credential, caDER []byte) error {
	if credential.Leaf == nil || credential.Certificate.PrivateKey == nil || len(caDER) == 0 {
		return errors.New("identity: incomplete active credential")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(credential.Certificate.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: credential.Leaf.Raw})
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{{"runtime-key.pem", keyPEM, 0600}, {"runtime-cert.pem", certPEM, 0644}, {"trust-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644}}
	for _, file := range files {
		if err := atomicCredentialFile(directory, file.name, file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

// PruneVersionFiles removes private keys and certificates that are no longer
// covered by the durable active/overlap set. Alias files are retained.
func PruneVersionFiles(directory string, keepSerials map[string]struct{}) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		var serial string
		switch {
		case strings.HasPrefix(name, "runtime-key-") && strings.HasSuffix(name, ".pem"):
			serial = strings.TrimSuffix(strings.TrimPrefix(name, "runtime-key-"), ".pem")
		case strings.HasPrefix(name, "runtime-cert-") && strings.HasSuffix(name, ".pem"):
			serial = strings.TrimSuffix(strings.TrimPrefix(name, "runtime-cert-"), ".pem")
		default:
			continue
		}
		if _, keep := keepSerials[serial]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func atomicCredentialFile(directory, name string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(directory, ".credential-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(directory, name)); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func VerifyDetachedSignature(publicKey ed25519.PublicKey, payload, signature []byte) error {
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, payload, signature) {
		return errors.New("identity: detached signature verification failed")
	}
	return nil
}

func TLSConfig(c Credential, roots *x509.CertPool, revoked func(*x509.Certificate) bool, server bool) (*tls.Config, error) {
	if c.Leaf == nil || c.Certificate.PrivateKey == nil {
		return nil, errors.New("identity: incomplete credential")
	}
	if roots == nil {
		return nil, errors.New("identity: trust pool required")
	}
	now := time.Now()
	if revoked != nil && revoked(c.Leaf) {
		return nil, errors.New("identity: local credential revoked")
	}
	if now.Before(c.Leaf.NotBefore) || !now.Before(c.Leaf.NotAfter) {
		return nil, errors.New("identity: local credential is outside validity interval")
	}
	conf := &tls.Config{SessionTicketsDisabled: true, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{c.Certificate}, RootCAs: roots, ClientCAs: roots, ServerName: c.NodeID}
	if server {
		conf.ClientAuth = tls.RequireAndVerifyClientCert
	}
	conf.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("identity: peer certificate missing")
		}
		now := time.Now()
		if now.Before(c.Leaf.NotBefore) || !now.Before(c.Leaf.NotAfter) || (revoked != nil && revoked(c.Leaf)) {
			return errors.New("identity: local credential no longer valid")
		}
		peer := state.PeerCertificates[0]
		if revoked != nil && revoked(peer) {
			return errors.New("identity: certificate revoked")
		}
		if peer.Subject.CommonName == "" || len(peer.DNSNames) != 1 || peer.DNSNames[0] != peer.Subject.CommonName {
			return errors.New("identity: canonical node_id SAN required")
		}
		return nil
	}
	return conf, nil
}

func TLSConfigSet(set CredentialSet, roots *x509.CertPool, revoked func(*x509.Certificate) bool, server bool, now time.Time) (*tls.Config, error) {
	credentials := set.ValidCredentials(now)
	if len(credentials) == 0 {
		return nil, errors.New("identity: no currently valid credentials")
	}
	config, err := TLSConfig(credentials[0], roots, revoked, server)
	if err != nil {
		return nil, err
	}
	config.Certificates = make([]tls.Certificate, 0, len(credentials))
	for _, credential := range credentials {
		if revoked != nil && revoked(credential.Leaf) {
			continue
		}
		if credential.Leaf == nil || credential.Certificate.PrivateKey == nil {
			return nil, errors.New("identity: incomplete overlapping credential")
		}
		config.Certificates = append(config.Certificates, credential.Certificate)
	}
	return config, nil
}

// ValidateRuntimeCredential enforces the approved identity-v1 profile. DNS-only
// legacy credentials can serve legacy control, but cannot enter VLink runtime.
func ValidateRuntimeCredential(c Credential, now time.Time) error {
	id, err := vlink.ParseNodeID(c.NodeID)
	if err != nil || id.String() != c.NodeID || c.Leaf == nil {
		return errors.New("identity: canonical node UUID required")
	}
	leaf := c.Leaf
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != c.NodeID || len(leaf.URIs) != 1 || leaf.URIs[0].String() != "urn:vortex:node:"+c.NodeID {
		return errors.New("identity: runtime DNS/URI SAN mismatch")
	}
	if leaf.IsCA || leaf.KeyUsage != x509.KeyUsageDigitalSignature || len(leaf.UnknownExtKeyUsage) != 0 || len(leaf.ExtKeyUsage) != 2 {
		return errors.New("identity: runtime certificate usage mismatch")
	}
	client, server := false, false
	for _, usage := range leaf.ExtKeyUsage {
		client = client || usage == x509.ExtKeyUsageClientAuth
		server = server || usage == x509.ExtKeyUsageServerAuth
	}
	if !client || !server {
		return errors.New("identity: both client and server EKU required")
	}
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return errors.New("identity: runtime credential outside validity")
	}
	_, err = CredentialFromTLS(c.NodeID, c.Certificate, leaf, c.PrivateKey)
	return err
}
