// Package rule implements Agent-owned, process-local rule transactions. It
// neither imports storage nor contacts the control plane.
package rule

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
)

type Target struct {
	Address  string `json:"address"`
	Weight   int32  `json:"weight"`
	Priority int32  `json:"priority"`
}
type Model struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Protocol           string   `json:"protocol"`
	Mode               string   `json:"mode"`
	Port               int32    `json:"port"`
	Path               []string `json:"path"`
	Directions         []string `json:"directions"`
	Targets            []Target `json:"targets"`
	Scheduling         string   `json:"scheduling"`
	ProxyProtocol      string   `json:"proxy_protocol"`
	IdleTimeoutSeconds int32    `json:"idle_timeout_seconds"`
	RateLimit          int64    `json:"rate_limit"`
	ConnectionLimit    int64    `json:"connection_limit"`
	Enabled            bool     `json:"enabled"`
}
type Member struct {
	ID            string `json:"id"`
	BootID        string `json:"boot_id"`
	PublicAddress string `json:"public_address"`
	Version       int64  `json:"version"`
	Generation    int64  `json:"generation"`
	LinkPort      int32  `json:"link_port"`
	Weight        int32  `json:"weight"`
	Priority      int32  `json:"priority"`
	Online        bool   `json:"online"`
}
type Segment struct {
	Hop           int    `json:"hop"`
	From          string `json:"from"`
	To            string `json:"to"`
	Direction     string `json:"direction"`
	PermitVersion int64  `json:"permit_version"`
}
type Intent struct {
	Rule            Model     `json:"rule"`
	Role            string    `json:"role"`
	Hop             int       `json:"hop_index"`
	AllowedUpstream []string  `json:"allowed_upstream_node_ids"`
	NextMembers     []Member  `json:"next_members"`
	Segments        []Segment `json:"segments"`
	SessionStable   bool      `json:"session_stable"`
}
type Artifact struct {
	TenantID          string            `json:"tenant_id"`
	RuleID            string            `json:"rule_id"`
	RuleRevision      int64             `json:"rule_revision"`
	NodeID            string            `json:"node_id"`
	BootID            string            `json:"boot_id"`
	SignerEpoch       int64             `json:"signer_epoch"`
	PathGeneration    int64             `json:"path_generation"`
	FencingGeneration int64             `json:"fencing_generation"`
	BillingSnapshotID string            `json:"billing_snapshot_id"`
	InputDigest       string            `json:"input_digest"`
	InputVersions     map[string]string `json:"input_versions"`
	Intent            Intent            `json:"intent"`
}

func Decode(raw []byte) (Artifact, error) {
	var a Artifact
	if len(raw) == 0 || len(raw) > 524288 {
		return a, errors.New("invalid_config: artifact size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&a); e != nil {
		return a, e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return a, errors.New("invalid_config: artifact trailing value")
	}
	canonical, e := json.Marshal(a)
	if e != nil || !bytes.Equal(canonical, raw) {
		return a, errors.New("invalid_config: noncanonical artifact")
	}
	m := a.Intent.Rule
	if a.RuleID == "" || m.ID != a.RuleID || a.RuleRevision < 1 || a.FencingGeneration < 1 || a.PathGeneration < 1 || a.SignerEpoch < 1 || a.NodeID == "" || a.BootID == "" || a.BillingSnapshotID == "" || !a.Intent.SessionStable || len(m.Targets) == 0 || len(m.Targets) > 256 {
		return a, errors.New("invalid_config: artifact bindings")
	}
	if m.Protocol != "tcp" && m.Protocol != "udp" && m.Protocol != "tcp_udp" {
		return a, errors.New("invalid_config: protocol")
	}
	if m.Port < 1 || m.Port > 65535 {
		return a, errors.New("invalid_config: unallocated port")
	}
	for _, t := range m.Targets {
		host, port, e := net.SplitHostPort(t.Address)
		p, x := strconv.Atoi(port)
		if e != nil || x != nil || host == "" || p < 1 || p > 65535 || t.Weight < 1 || t.Weight > 10000 || t.Priority < 0 {
			return a, errors.New("invalid_config: target")
		}
	}
	return a, nil
}
