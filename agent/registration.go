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
	key, csr, err := identity.GenerateCSR(nodeID)
	if err != nil {
		return identity.Credential{}, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: serverName}
	conn, err := (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", address)
	if err != nil {
		return identity.Credential{}, fmt.Errorf("agent registration: connect: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(registrationRequest{Type: "register", Token: token, NodeID: nodeID, CSR: csr}); err != nil {
		return identity.Credential{}, err
	}
	var response registrationResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return identity.Credential{}, err
	}
	if response.Type != "registered" || len(response.Certificate) == 0 || len(response.CA) == 0 {
		return identity.Credential{}, fmt.Errorf("agent registration: rejected: %s", response.Error)
	}
	leaf, err := x509.ParseCertificate(response.Certificate)
	if err != nil {
		return identity.Credential{}, fmt.Errorf("agent registration: parse issued certificate: %w", err)
	}
	chain, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil {
		return identity.Credential{}, fmt.Errorf("agent registration: issued certificate is not trusted: %w", err)
	}
	if len(chain) == 0 {
		return identity.Credential{}, errors.New("agent registration: issued certificate has no trusted chain")
	}
	credential, err := identity.AttachKey(identity.Credential{NodeID: nodeID, Leaf: leaf}, key, response.CA)
	if err != nil {
		return identity.Credential{}, err
	}
	if err := identity.SaveFiles(certificateDir, credential, response.CA); err != nil {
		return identity.Credential{}, err
	}
	if err := PersistCredential(ctx, db, credential); err != nil {
		return identity.Credential{}, err
	}
	return credential, nil
}
