package config

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"vortex/engine/vlink"
)

type Role string

const (
	RoleSource      Role = "source"
	RoleRelay       Role = "relay"
	RoleDestination Role = "destination"
)

// Route is a locally authorized segment. Candidate definitions are pinned at startup.
type Route struct {
	Peer          string `json:"peer"`
	PeerNodeID    string `json:"peer_node_id"`
	NextHopNodeID string `json:"next_hop_node_id"`
	HopIndex      uint16 `json:"hop_index"`
}

type Config struct {
	Routes           map[string]Route    `json:"routes,omitempty"`
	ActiveRoute      string              `json:"active_route,omitempty"`
	Version          uint64              `json:"version"`
	NodeID           string              `json:"node_id"`
	Role             Role                `json:"role"`
	Certificate      string              `json:"certificate"`
	PrivateKey       string              `json:"private_key"`
	TrustBundle      string              `json:"trust_bundle"`
	Listen           string              `json:"listen"`
	Peer             string              `json:"peer,omitempty"`
	PeerNodeID       string              `json:"peer_node_id,omitempty"`
	NextHopNodeID    string              `json:"next_hop_node_id,omitempty"`
	ServiceListen    string              `json:"service_listen,omitempty"`
	UDPServiceListen string              `json:"udp_service_listen,omitempty"`
	ExecutionPoint   bool                `json:"execution_point,omitempty"`
	RuleID           string              `json:"rule_id"`
	RuleRevision     uint64              `json:"rule_revision"`
	PathGeneration   uint64              `json:"path_generation"`
	HopIndex         uint16              `json:"hop_index"`
	Target           string              `json:"target,omitempty"`
	EvidenceFile     string              `json:"evidence_file,omitempty"`
	TransportMode    vlink.TransportMode `json:"transport_mode,omitempty"`
	PoolMinLinks     int                 `json:"pool_min_links,omitempty"`
	PoolMaxLinks     int                 `json:"pool_max_links,omitempty"`
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: decode: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, errors.New("config: multiple JSON documents")
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if len(c.Routes) > 16 {
		return errors.New("config: maximum 16 authorized routes")
	}
	if len(c.Routes) > 0 {
		if c.TransportMode == "" {
			return errors.New("config: routes require transport_mode")
		}
		if _, ok := c.Routes[c.ActiveRoute]; !ok {
			return errors.New("config: active_route is not authorized")
		}
		for id, r := range c.Routes {
			if id == "" {
				return errors.New("config: empty route id")
			}
			if c.Role != RoleDestination {
				if _, _, err := net.SplitHostPort(r.Peer); err != nil {
					return errors.New("config: invalid route peer")
				}
				if _, err := vlink.ParseNodeID(r.NextHopNodeID); err != nil {
					return errors.New("config: invalid route next hop")
				}
			}
			if _, err := vlink.ParseNodeID(r.PeerNodeID); err != nil {
				return errors.New("config: invalid route peer identity")
			}
		}
	} else if c.ActiveRoute != "" {
		return errors.New("config: active_route requires routes")
	}

	if c.TransportMode != "" && c.TransportMode != vlink.ModePool && c.TransportMode != vlink.ModeMux && c.TransportMode != vlink.ModeAuto {
		return errors.New("config: transport_mode must be pool, mux, or auto")
	}
	if c.PoolMinLinks < 0 || c.PoolMaxLinks < 0 || c.PoolMaxLinks > 16 || c.PoolMinLinks > c.PoolBounds().MaxLinks {
		return errors.New("config: invalid pool bounds (maximum 16 links)")
	}
	if c.TransportMode == "" && (c.PoolMinLinks != 0 || c.PoolMaxLinks != 0) {
		return errors.New("config: pool bounds require an explicit transport_mode")
	}
	if c.Version == 0 {
		return errors.New("config: version must be positive")
	}
	if _, err := vlink.ParseNodeID(c.NodeID); err != nil {
		return fmt.Errorf("config: node_id: %w", err)
	}
	switch c.Role {
	case RoleSource, RoleRelay, RoleDestination:
	default:
		return fmt.Errorf("config: unsupported role %q", c.Role)
	}
	if c.Certificate == "" || c.PrivateKey == "" || c.TrustBundle == "" {
		return errors.New("config: certificate, private_key and trust_bundle are required")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("config: listen: %w", err)
	}
	if c.Role != RoleDestination && c.Peer == "" {
		return errors.New("config: peer is required for source and relay roles")
	}
	if c.Role == RoleSource && c.ServiceListen == "" {
		return errors.New("config: service_listen is required for source role")
	}
	if c.UDPServiceListen != "" {
		if c.Role != RoleSource {
			return errors.New("config: udp_service_listen is only valid for source role")
		}
		if _, _, err := net.SplitHostPort(c.UDPServiceListen); err != nil {
			return fmt.Errorf("config: udp_service_listen: %w", err)
		}
	}
	if c.Role == RoleDestination && c.Target == "" {
		return errors.New("config: target is required for destination role")
	}
	if c.RuleID == "" || c.RuleRevision == 0 || c.PathGeneration == 0 {
		return errors.New("config: rule_id, rule_revision and path_generation are required")
	}
	return nil
}

func (c Config) PoolBounds() vlink.PoolConfig {
	minimum, maximum := c.PoolMinLinks, c.PoolMaxLinks
	if minimum == 0 {
		minimum = 1
	}
	if maximum == 0 {
		maximum = 4
	}
	return vlink.PoolConfig{MinLinks: minimum, MaxLinks: maximum, HardCap: maximum}
}

func (c Config) TLSConfig(server bool) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(c.Certificate, c.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("config: load certificate: %w", err)
	}
	data, err := os.ReadFile(c.TrustBundle)
	if err != nil {
		return nil, fmt.Errorf("config: read trust bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("config: trust bundle contains no certificates")
	}
	// `vortex` is a certificate validation name shared by the qualification
	// trust domain; the node identity itself is still checked from the URI SAN
	// by HandshakeHello.
	result := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, RootCAs: pool, ClientCAs: pool, ServerName: "vortex"}
	if server {
		result.ClientAuth = tls.RequireAndVerifyClientCert
	} else {
		result.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return result, nil
}

type Identity struct {
	NodeID vlink.NodeID
	Signer crypto.Signer
	TLS    *tls.Config
}

func (c Config) LoadIdentity(server bool) (Identity, error) {
	tlsConfig, err := c.TLSConfig(server)
	if err != nil {
		return Identity{}, err
	}
	if len(tlsConfig.Certificates) != 1 || tlsConfig.Certificates[0].PrivateKey == nil {
		return Identity{}, errors.New("config: exactly one certificate with private key is required")
	}
	cert, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		return Identity{}, fmt.Errorf("config: parse local certificate: %w", err)
	}
	nodeID, err := vlink.NodeIDFromCertificate(cert)
	if err != nil {
		return Identity{}, fmt.Errorf("config: local certificate identity: %w", err)
	}
	wanted, err := vlink.ParseNodeID(c.NodeID)
	if err != nil || nodeID != wanted {
		return Identity{}, errors.New("config: certificate node_id does not match config")
	}
	signer, ok := tlsConfig.Certificates[0].PrivateKey.(crypto.Signer)
	if !ok {
		return Identity{}, errors.New("config: certificate private key is not a crypto signer")
	}
	return Identity{NodeID: nodeID, Signer: signer, TLS: tlsConfig}, nil
}

func (c Config) Effective() Config {
	if r, ok := c.Routes[c.ActiveRoute]; ok {
		c.Peer = r.Peer
		c.PeerNodeID = r.PeerNodeID
		c.NextHopNodeID = r.NextHopNodeID
		c.HopIndex = r.HopIndex
	}
	return c
}
