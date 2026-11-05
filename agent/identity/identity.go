package identity

import (
	"bytes"
	"context"
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
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Credential struct {
	NodeID      string
	PrivateKey  *ecdsa.PrivateKey
	Certificate tls.Certificate
	Leaf        *x509.Certificate
}

type CA struct {
	Certificate *x509.Certificate
	Key         *ecdsa.PrivateKey
}

type Enroller struct {
	CA     *CA
	Tokens map[string]bool
}

func (e *Enroller) Redeem(token, nodeID string, csrDER []byte, now time.Time, ttl time.Duration) (Credential, error) {
	if e == nil || e.CA == nil || token == "" || !e.Tokens[token] {
		return Credential{}, errors.New("identity: invalid or consumed enrollment token")
	}
	delete(e.Tokens, token)
	cred, err := e.CA.SignCSR(csrDER, nodeID, now, ttl)
	if err != nil {
		return Credential{}, err
	}
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
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID}, DNSNames: []string{nodeID}}, key)
	if err != nil {
		return nil, nil, err
	}
	return key, csr, nil
}

func (ca *CA) SignCSR(csrDER []byte, nodeID string, now time.Time, ttl time.Duration) (Credential, error) {
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
	priv, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Credential{}, err
	}
	_ = caDER
	c.Certificate.PrivateKey = key
	c.Certificate.Certificate = [][]byte{c.Leaf.Raw}
	c.Certificate.Leaf = c.Leaf
	_ = priv
	return c, nil
}

func SaveFiles(directory string, credential Credential, caDER []byte) error {
	if credential.Leaf == nil || credential.Certificate.PrivateKey == nil {
		return errors.New("identity: incomplete credential")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(credential.Certificate.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: credential.Leaf.Raw})
	if err := os.WriteFile(filepath.Join(directory, "runtime-key.pem"), keyPEM, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "runtime-cert.pem"), certPEM, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "trust-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644)
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
	conf := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{c.Certificate}, RootCAs: roots, ClientCAs: roots, ServerName: c.NodeID, ClientAuth: tls.RequireAndVerifyClientCert}
	conf.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("identity: peer certificate missing")
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
	_ = context.Background()
	_ = server
	return conf, nil
}
