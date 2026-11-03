package config

import (
	"bytes"
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

type Config struct {
	Version        uint64 `json:"version"`
	NodeID         string `json:"node_id"`
	Role           Role   `json:"role"`
	Certificate    string `json:"certificate"`
	PrivateKey     string `json:"private_key"`
	TrustBundle    string `json:"trust_bundle"`
	Listen         string `json:"listen"`
	Peer           string `json:"peer,omitempty"`
	RuleID         string `json:"rule_id"`
	RuleRevision   uint64 `json:"rule_revision"`
	PathGeneration uint64 `json:"path_generation"`
	HopIndex       uint16 `json:"hop_index"`
	Target         string `json:"target,omitempty"`
	EvidenceFile   string `json:"evidence_file,omitempty"`
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
	if c.Role == RoleDestination && c.Target == "" {
		return errors.New("config: target is required for destination role")
	}
	if c.RuleID == "" || c.RuleRevision == 0 || c.PathGeneration == 0 {
		return errors.New("config: rule_id, rule_revision and path_generation are required")
	}
	return nil
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
	result := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, RootCAs: pool, ClientCAs: pool, ServerName: "vortex"}
	if server {
		result.ClientAuth = tls.RequireAndVerifyClientCert
	} else {
		result.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return result, nil
}
