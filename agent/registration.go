package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
)

type registrationRequest struct {
	Type   string `json:"type"`
	Token  string `json:"token"`
	NodeID string `json:"node_id"`
	CSR    []byte `json:"csr_der"`
}

type registrationResponse struct {
	Type        string `json:"type"`
	Certificate []byte `json:"certificate_der"`
	CA          []byte `json:"ca_der"`
	Error       string `json:"error,omitempty"`
}

// Register generates the private key locally, redeems a one-time enrollment
// token over server-authenticated TLS, validates the issued identity, and
// persists the key material before returning success.
func Register(ctx context.Context, nodeID, token, address, certificateDir string, roots *x509.CertPool, serverName string, db *store.Store) (identity.Credential, error) {
	if nodeID == "" || token == "" || address == "" || certificateDir == "" || roots == nil || serverName == "" || db == nil {
		return identity.Credential{}, errors.New("agent registration: node, token, endpoint, TLS trust, certificate path and store are required")
	}
	credential, caDER, err := exchangeCredential(ctx, nodeID, token, address, roots, serverName, nil)
	if err != nil {
		return identity.Credential{}, err
	}
	if err := identity.SaveFiles(certificateDir, credential, caDER); err != nil {
		return identity.Credential{}, err
	}
	if err := PersistCredential(ctx, db, credential); err != nil {
		return identity.Credential{}, err
	}
	return credential, nil
}

// Renew authenticates with the current credential and obtains a replacement
// key and certificate from the same trusted enrollment endpoint.
func Renew(ctx context.Context, current identity.Credential, address, certificateDir string, roots *x509.CertPool, serverName string, db *store.Store) (identity.Credential, error) {
	if current.NodeID == "" || current.Leaf == nil || current.Certificate.PrivateKey == nil || address == "" || certificateDir == "" || roots == nil || serverName == "" || db == nil {
		return identity.Credential{}, errors.New("agent renewal: current credential, endpoint, TLS trust, certificate path and store are required")
	}
	credential, caDER, err := exchangeCredential(ctx, current.NodeID, "", address, roots, serverName, &current)
	if err != nil {
		return identity.Credential{}, err
	}
	if err := persistCredentialRotation(ctx, db, certificateDir, credential, caDER, current, roots, time.Now().UTC(), time.Minute); err != nil {
		return identity.Credential{}, err
	}
	return credential, nil
}

func persistCredentialRotation(ctx context.Context, db *store.Store, directory string, next identity.Credential, caDER []byte, current identity.Credential, roots *x509.CertPool, now time.Time, overlap time.Duration) error {
	if db == nil || next.Leaf == nil || current.Leaf == nil || next.NodeID != current.NodeID || roots == nil || overlap <= 0 {
		return errors.New("agent renewal: valid current and replacement credentials plus positive overlap are required")
	}
	set, err := (identity.CredentialSet{Active: current}).Rotate(next, now, overlap, func(candidate identity.Credential) error {
		if _, err := candidate.Leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := identity.SaveVersionFiles(directory, next); err != nil {
		return err
	}
	if err := db.SaveCredentialRotation(ctx, next.NodeID, set.Active.Leaf.Raw, set.Active.Leaf.NotAfter, set.Previous.Leaf.Raw, set.Previous.Leaf.NotAfter, set.PreviousUntil); err != nil {
		return err
	}
	return identity.SaveActiveFiles(directory, next, caDER)
}

func exchangeCredential(ctx context.Context, nodeID, token, address string, roots *x509.CertPool, serverName string, current *identity.Credential) (identity.Credential, []byte, error) {
	key, csr, err := identity.GenerateCSR(nodeID)
	if err != nil {
		return identity.Credential{}, nil, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: serverName}
	requestType := "register"
	if current != nil {
		config.Certificates = []tls.Certificate{current.Certificate}
		requestType = "renew"
	}
	conn, err := (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", address)
	if err != nil {
		return identity.Credential{}, nil, fmt.Errorf("agent registration: connect: %w", err)
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(registrationRequest{Type: requestType, Token: token, NodeID: nodeID, CSR: csr}); err != nil {
		return identity.Credential{}, nil, err
	}
	var response registrationResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return identity.Credential{}, nil, err
	}
	if response.Type != "registered" || len(response.Certificate) == 0 || len(response.CA) == 0 {
		return identity.Credential{}, nil, fmt.Errorf("agent registration: rejected: %s", response.Error)
	}
	leaf, err := x509.ParseCertificate(response.Certificate)
	if err != nil {
		return identity.Credential{}, nil, fmt.Errorf("agent registration: parse issued certificate: %w", err)
	}
	chain, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil {
		return identity.Credential{}, nil, fmt.Errorf("agent registration: issued certificate is not trusted: %w", err)
	}
	if len(chain) == 0 {
		return identity.Credential{}, nil, errors.New("agent registration: issued certificate has no trusted chain")
	}
	credential, err := identity.AttachKey(identity.Credential{NodeID: nodeID, Leaf: leaf}, key, response.CA)
	if err != nil {
		return identity.Credential{}, nil, err
	}
	return credential, response.CA, nil
}
