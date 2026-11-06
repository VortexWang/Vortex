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
	"time"

	runtimeaccounting "vortex/engine/runtime/accounting"
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

// PolicyWindowConfig describes an injected, process-local policy transition.
// It is intentionally explicit: runtime defaults never infer a billing window.
type PolicyWindowConfig struct {
	OldSnapshot       string    `json:"old_snapshot"`
	NewSnapshot       string    `json:"new_snapshot"`
	At                time.Time `json:"at"`
	PreparedAt        time.Time `json:"prepared_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	PolicyLeaseUntil  time.Time `json:"policy_lease_until"`
	RuntimeLeaseUntil time.Time `json:"runtime_lease_until"`
	ShareLeaseUntil   time.Time `json:"share_lease_until"`
	HardDeadline      time.Time `json:"hard_deadline"`
	SelectionRevision uint64    `json:"selection_revision"`
	UncertaintyMS     int64     `json:"uncertainty_ms"`
	IssuerTimeoutMS   int64     `json:"issuer_timeout_ms"`
}

// ShareConfig describes an injected, process-local connection-share lease.
// The authoritative Prepare/Commit record remains outside Engine; this
// payload only permits a runtime to prove the boot and transaction it was
// started with before admitting flows.
type ShareConfig struct {
	BootID        string    `json:"boot_id"`
	TransactionID string    `json:"transaction_id"`
	Revision      uint64    `json:"revision,omitempty"`
	Rate          int64     `json:"rate"`
	Connections   int64     `json:"connections"`
	LeaseUntil    time.Time `json:"lease_until"`
}

type Config struct {
	SourceCreditBook  *runtimeaccounting.BookConfig   `json:"source_credit_book,omitempty"`
	SourceCredit      *runtimeaccounting.CreditConfig `json:"source_credit,omitempty"`
	PolicyWindow      *PolicyWindowConfig             `json:"policy_window,omitempty"`
	Share             *ShareConfig                    `json:"share,omitempty"`
	QualificationOnly bool                            `json:"qualification_only,omitempty"`
	Routes            map[string]Route                `json:"routes,omitempty"`
	ActiveRoute       string                          `json:"active_route,omitempty"`
	Version           uint64                          `json:"version"`
	NodeID            string                          `json:"node_id"`
	Role              Role                            `json:"role"`
	Certificate       string                          `json:"certificate"`
	PrivateKey        string                          `json:"private_key"`
	TrustBundle       string                          `json:"trust_bundle"`
	Listen            string                          `json:"listen"`
	Peer              string                          `json:"peer,omitempty"`
	PeerNodeID        string                          `json:"peer_node_id,omitempty"`
	NextHopNodeID     string                          `json:"next_hop_node_id,omitempty"`
	ServiceListen     string                          `json:"service_listen,omitempty"`
	UDPServiceListen  string                          `json:"udp_service_listen,omitempty"`
	ExecutionPoint    bool                            `json:"execution_point,omitempty"`
	RuleID            string                          `json:"rule_id"`
	RuleRevision      uint64                          `json:"rule_revision"`
	PathGeneration    uint64                          `json:"path_generation"`
	HopIndex          uint16                          `json:"hop_index"`
	Target            string                          `json:"target,omitempty"`
	EvidenceFile      string                          `json:"evidence_file,omitempty"`
	TransportMode     vlink.TransportMode             `json:"transport_mode,omitempty"`
	PoolMinLinks      int                             `json:"pool_min_links,omitempty"`
	PoolMaxLinks      int                             `json:"pool_max_links,omitempty"`
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
	if c.Role == RoleSource && c.SourceCreditBook == nil && c.SourceCredit == nil && !c.QualificationOnly {
		return errors.New("config: source role requires a funded source credit gate")
	}
	if c.QualificationOnly && c.EvidenceFile == "" {
		return errors.New("config: qualification_only requires evidence_file")
	}
	if c.Share != nil {
		if c.Role != RoleSource || !c.ExecutionPoint || c.SourceCreditBook == nil && c.SourceCredit == nil {
			return errors.New("config: share requires a funded source execution point")
		}
		if _, err := vlink.ParseNodeID(c.Share.BootID); err != nil {
			return fmt.Errorf("config: share boot_id: %w", err)
		}
		if _, err := vlink.ParseNodeID(c.Share.TransactionID); err != nil {
			return fmt.Errorf("config: share transaction_id: %w", err)
		}
		if c.Share.Rate <= 0 || c.Share.Connections <= 0 {
			return errors.New("config: share rate and connections must be positive")
		}
		if c.Share.Revision > 0 && c.Share.Revision != 1 {
			return errors.New("config: share revision must match the first process-local fencing revision")
		}
		if c.Share.LeaseUntil.IsZero() {
			return errors.New("config: share lease_until is required")
		}
	}
	if c.SourceCreditBook != nil {
		if err := c.SourceCreditBook.Validate(); err != nil {
			return err
		}
		if c.SourceCreditBook.Snapshots[c.SourceCreditBook.Active].Credit.Budget == 0 {
			return errors.New("config: active source credit snapshot must have a funded budget")
		}
		if c.SourceCredit != nil || c.Role != RoleSource || !c.ExecutionPoint || len(c.Routes) > 0 || c.EvidenceFile == "" || c.TransportMode == "" {
			return errors.New("config: credit book requires exclusive static source authority")
		}
		for _, v := range c.SourceCreditBook.Snapshots {
			if v.Binding.RuleID != c.RuleID || v.Binding.ExecutionNode != c.NodeID || v.Binding.PathGeneration != c.PathGeneration || v.Credit.MaxBlock < 64*1024 {
				return errors.New("config: credit snapshot differs from pinned execution path")
			}
		}
	}
	if c.PolicyWindow != nil {
		if c.SourceCreditBook == nil {
			return errors.New("config: policy_window requires source_credit_book")
		}
		w := c.PolicyWindow
		window := runtimeaccounting.PolicyWindow{
			OldSnapshot: w.OldSnapshot, NewSnapshot: w.NewSnapshot, At: w.At,
			PreparedAt: w.PreparedAt, ExpiresAt: w.ExpiresAt,
			PolicyLeaseUntil: w.PolicyLeaseUntil, RuntimeLeaseUntil: w.RuntimeLeaseUntil,
			ShareLeaseUntil: w.ShareLeaseUntil, HardDeadline: w.HardDeadline,
			SelectionRevision: w.SelectionRevision,
		}
		if err := window.Validate(); err != nil {
			return fmt.Errorf("config: policy_window: %w", err)
		}
		if w.UncertaintyMS < 0 || w.UncertaintyMS > 10000 {
			return errors.New("config: policy_window uncertainty_ms must be 0..10000")
		}
		if w.IssuerTimeoutMS < 1 || w.IssuerTimeoutMS > 200 {
			return errors.New("config: policy_window issuer_timeout_ms must be 1..200")
		}
	}

	if c.SourceCredit != nil {
		if err := c.SourceCredit.Validate(); err != nil {
			return err
		}
		if c.SourceCredit.Budget == 0 {
			return errors.New("config: source_credit budget must be funded")
		}
		if c.Role != RoleSource || !c.ExecutionPoint || len(c.Routes) > 0 || c.EvidenceFile == "" || c.TransportMode == "" || c.SourceCredit.MaxBlock < 64*1024 {
			return errors.New("config: source_credit requires static source execution point, explicit transport, max block >= 64 KiB and evidence_file")
		}
	}

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
	return c.TLSConfigWithCertificate(server, cert)
}

// TLSConfigWithCertificate loads pinned runtime trust without opening identity
// alias files. Agent supplies its durably selected certificate via the provider.
func (c Config) TLSConfigWithCertificate(server bool, cert tls.Certificate) (*tls.Config, error) {
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
