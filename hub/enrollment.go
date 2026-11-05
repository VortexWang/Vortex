package hub

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"vortex/agent/identity"
)

type enrollmentRequest struct {
	Type   string `json:"type"`
	Token  string `json:"token"`
	NodeID string `json:"node_id"`
	CSR    []byte `json:"csr_der"`
}

type enrollmentResponse struct {
	Type        string `json:"type"`
	Certificate []byte `json:"certificate_der,omitempty"`
	CA          []byte `json:"ca_der,omitempty"`
	Error       string `json:"error,omitempty"`
}

// EnrollmentServer is a bounded test/bootstrap endpoint. Production enrollment
// authority and durable token lifecycle belong to the backend implementation.
type EnrollmentServer struct {
	Enroller *identity.Enroller
	CA       []byte
	TLS      *tls.Config
	TTL      time.Duration
	Roots    *x509.CertPool
	mu       sync.RWMutex
	revoked  map[string]struct{}
}

func (s *EnrollmentServer) Serve(ctx context.Context, listener net.Listener) error {
	if s == nil || s.Enroller == nil || s.TLS == nil || s.TTL <= 0 {
		return errors.New("hub enrollment: incomplete test server configuration")
	}
	listener = tls.NewListener(listener, s.TLS.Clone())
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.handle(conn)
	}
}

func (s *EnrollmentServer) handle(conn net.Conn) {
	defer conn.Close()
	var request enrollmentRequest
	decoder := json.NewDecoder(bufio.NewReader(conn))
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "invalid request"})
		return
	}
	if request.Type == "renew" {
		s.renew(conn, request)
		return
	}
	if request.Type != "register" {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "unsupported request"})
		return
	}
	credential, err := s.Enroller.Redeem(request.Token, request.NodeID, request.CSR, time.Now().UTC(), s.TTL)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "registration rejected"})
		return
	}
	if err := json.NewEncoder(conn).Encode(enrollmentResponse{Type: "registered", Certificate: credential.Leaf.Raw, CA: s.CA}); err != nil {
		return
	}
}

func (s *EnrollmentServer) renew(conn net.Conn, request enrollmentRequest) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "authenticated renewal required"})
		return
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 || request.NodeID == "" {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "authenticated renewal required"})
		return
	}
	peer := state.PeerCertificates[0]
	if _, err := peer.Verify(x509.VerifyOptions{Roots: s.Roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil || len(peer.DNSNames) != 1 || peer.DNSNames[0] != request.NodeID {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "credential rejected"})
		return
	}
	s.mu.RLock()
	_, revoked := s.revoked[peer.SerialNumber.String()]
	s.mu.RUnlock()
	if revoked {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "credential revoked"})
		return
	}
	credential, err := s.Enroller.CA.SignCSR(request.CSR, request.NodeID, time.Now().UTC(), s.TTL)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "error", Error: "renewal rejected"})
		return
	}
	_ = json.NewEncoder(conn).Encode(enrollmentResponse{Type: "registered", Certificate: credential.Leaf.Raw, CA: s.CA})
}

func (s *EnrollmentServer) Revoke(serial string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked == nil {
		s.revoked = make(map[string]struct{})
	}
	s.revoked[serial] = struct{}{}
}

func NewEnrollmentServer(ca *identity.CA, caDER []byte, tokens map[string]bool, tlsConfig *tls.Config, ttl time.Duration) (*EnrollmentServer, error) {
	if ca == nil || len(caDER) == 0 || len(tokens) == 0 || tlsConfig == nil || ttl <= 0 {
		return nil, fmt.Errorf("hub enrollment: CA, tokens, TLS and positive TTL are required")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	config := tlsConfig.Clone()
	config.MinVersion = tls.VersionTLS13
	config.MaxVersion = tls.VersionTLS13
	config.ClientAuth = tls.RequestClientCert
	return &EnrollmentServer{Enroller: &identity.Enroller{CA: ca, Tokens: tokens}, CA: append([]byte(nil), caDER...), TLS: config, TTL: ttl, Roots: roots, revoked: make(map[string]struct{})}, nil
}
