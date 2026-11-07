// Package compiler is the sole pure transformation from locked business input
// into runtime intent. It never reads a database, clock, catalog or network.
package compiler

import (
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Target struct {
	Address  string `json:"address"`
	Weight   int32  `json:"weight"`
	Priority int32  `json:"priority"`
}
type Rule struct {
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
type Diagnostic string

func (d Diagnostic) Error() string { return string(d) }

const (
	Invalid          Diagnostic = "RULE_INVALID"
	Unreachable      Diagnostic = "RULE_SEGMENT_UNREACHABLE"
	Unauthorized     Diagnostic = "RULE_AUTHORIZATION_REQUIRED"
	RevisionConflict Diagnostic = "RULE_REVISION_CONFLICT"
)

// NormalizeRule preserves path order and canonicalizes only unordered values.
// Target hostnames are resolved by the exit at session admission, where the
// resolved address must pass its destination security policy again.
func NormalizeRule(in Rule) (Rule, error) {
	in.Path = append([]string(nil), in.Path...)
	in.Directions = append([]string(nil), in.Directions...)
	in.Targets = append([]Target(nil), in.Targets...)
	id, e := uuid.Parse(in.ID)
	if e != nil || id == uuid.Nil {
		return Rule{}, Invalid
	}
	in.ID = id.String()
	if len(strings.TrimSpace(in.Name)) == 0 || len(in.Name) > 128 || in.Port < 0 || in.Port > 65535 || in.RateLimit < 0 || in.ConnectionLimit < 0 {
		return Rule{}, Invalid
	}
	if in.Protocol != "tcp" && in.Protocol != "udp" && in.Protocol != "tcp_udp" {
		return Rule{}, Invalid
	}
	if in.Mode != "direct" && in.Mode != "tunnel" && in.Mode != "multi_hop" {
		return Rule{}, Invalid
	}
	if in.Scheduling != "weighted_round_robin" && in.Scheduling != "primary_backup" {
		return Rule{}, Invalid
	}
	if in.ProxyProtocol != "off" && in.ProxyProtocol != "v1" && in.ProxyProtocol != "v2" {
		return Rule{}, Invalid
	}
	if in.Protocol != "tcp" && in.ProxyProtocol == "v1" {
		return Rule{}, Invalid
	}
	if in.IdleTimeoutSeconds == 0 {
		in.IdleTimeoutSeconds = 60
	}
	if in.IdleTimeoutSeconds < 10 || in.IdleTimeoutSeconds > 86400 {
		return Rule{}, Invalid
	}
	if len(in.Path) < 1 || len(in.Path) > 32 || (in.Mode == "direct" && len(in.Path) != 1) || (in.Mode == "tunnel" && len(in.Path) != 2) || (in.Mode == "multi_hop" && len(in.Path) < 3) {
		return Rule{}, Invalid
	}
	seen := map[string]bool{}
	for i, s := range in.Path {
		id, e := uuid.Parse(s)
		if e != nil || id == uuid.Nil || seen[id.String()] {
			return Rule{}, Invalid
		}
		in.Path[i] = id.String()
		seen[id.String()] = true
	}
	if len(in.Directions) == 0 {
		in.Directions = make([]string, len(in.Path)-1)
	}
	if len(in.Directions) != len(in.Path)-1 {
		return Rule{}, Invalid
	}
	for i, d := range in.Directions {
		if d == "" {
			d = "auto"
		}
		if d != "auto" && d != "forward" && d != "reverse" {
			return Rule{}, Invalid
		}
		in.Directions[i] = d
	}
	if len(in.Targets) < 1 || len(in.Targets) > 256 {
		return Rule{}, Invalid
	}
	seen = map[string]bool{}
	for i, t := range in.Targets {
		host, port, e := net.SplitHostPort(t.Address)
		if e != nil || host == "" || t.Weight < 1 || t.Weight > 10000 || t.Priority < 0 {
			return Rule{}, Invalid
		}
		p, e := strconv.Atoi(port)
		if e != nil || p < 1 || p > 65535 || port != strings.TrimSpace(port) {
			return Rule{}, Invalid
		}
		port = strconv.Itoa(p)
		// Numeric ports only: normalization must never consult service databases.
		for _, c := range port {
			if c < '0' || c > '9' {
				return Rule{}, Invalid
			}
		}
		if ip, e := netip.ParseAddr(host); e == nil {
			host = ip.Unmap().String()
			if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return Rule{}, Invalid
			}
		} else {
			host = strings.ToLower(strings.TrimSuffix(host, "."))
			if !validHost(host) {
				return Rule{}, Invalid
			}
		}
		in.Targets[i].Address = net.JoinHostPort(host, port)
		if seen[in.Targets[i].Address] {
			return Rule{}, Invalid
		}
		seen[in.Targets[i].Address] = true
	}
	sort.Slice(in.Targets, func(i, j int) bool { return in.Targets[i].Address < in.Targets[j].Address })
	return in, nil
}
func validHost(s string) bool {
	if len(s) > 253 || len(s) == 0 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func ChangeClass(old, next Rule) string {
	if !next.Enabled {
		return "stop"
	}
	if old.Protocol != next.Protocol || old.Mode != next.Mode || old.Port != next.Port || strings.Join(old.Path, ",") != strings.Join(next.Path, ",") || strings.Join(old.Directions, ",") != strings.Join(next.Directions, ",") || old.ProxyProtocol != next.ProxyProtocol {
		return "rebuild"
	}
	return "hot"
}

var _ error = Invalid
