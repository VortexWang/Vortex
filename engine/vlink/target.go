package vlink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"

	"vortex/engine/accounting"
)

var ErrTargetRejected = errors.New("vlink: local target rejected")

type LocalTarget struct {
	Host            string
	Port            uint16
	AllowedPrefixes []netip.Prefix
}
type TargetResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type systemTargetResolver struct{}

func (systemTargetResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

var forbiddenTargetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"), netip.MustParsePrefix("2001:db8::/32"),
}

// ResolveLocalTarget resolves once, validates every answer, and returns a
// numeric endpoint so the eventual dial cannot trigger a second DNS lookup.
func ResolveLocalTarget(ctx context.Context, target LocalTarget, resolver TargetResolver) (netip.AddrPort, error) {
	if target.Port == 0 || target.Host == "" || strings.ContainsAny(target.Host, "/\\\x00") {
		return netip.AddrPort{}, ErrTargetRejected
	}
	if resolver == nil {
		resolver = systemTargetResolver{}
	}
	addresses := []netip.Addr{}
	if address, err := netip.ParseAddr(target.Host); err == nil {
		addresses = append(addresses, address)
	} else {
		if !validDNSName(target.Host) {
			return netip.AddrPort{}, ErrTargetRejected
		}
		resolved, err := resolver.LookupNetIP(ctx, "ip", target.Host)
		if err != nil || len(resolved) == 0 {
			return netip.AddrPort{}, ErrTargetRejected
		}
		addresses = resolved
	}
	for _, address := range addresses {
		address = address.Unmap()
		if forbiddenAddress(address) {
			return netip.AddrPort{}, ErrTargetRejected
		}
		if len(target.AllowedPrefixes) > 0 {
			allowed := false
			for _, prefix := range target.AllowedPrefixes {
				if prefix.Contains(address) {
					allowed = true
					break
				}
			}
			if !allowed {
				return netip.AddrPort{}, ErrTargetRejected
			}
		}
	}
	return netip.AddrPortFrom(addresses[0].Unmap(), target.Port), nil
}

func forbiddenAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range forbiddenTargetPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func DialLocalTarget(ctx context.Context, network string, endpoint netip.AddrPort) (net.Conn, error) {
	if !endpoint.IsValid() || forbiddenAddress(endpoint.Addr()) || endpoint.Port() == 0 || (network != "tcp" && network != "tcp4" && network != "tcp6" && network != "udp" && network != "udp4" && network != "udp6") {
		return nil, ErrTargetRejected
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, endpoint.String())
	if err != nil {
		return nil, ErrTargetRejected
	}
	return conn, nil
}

type TargetResource struct {
	Conn     net.Conn
	Policy   accounting.Session
	once     sync.Once
	closeErr error
}

func (r *TargetResource) Close() error {
	r.once.Do(func() {
		if r.Conn != nil {
			r.closeErr = errors.Join(r.closeErr, r.Conn.Close())
		}
		if r.Policy != nil {
			r.closeErr = errors.Join(r.closeErr, r.Policy.Close())
		}
	})
	return r.closeErr
}

type TCPAdmission struct {
	Policy   OpenPolicy
	Resolver TargetResolver
	Gate     accounting.Gate
	Dial     func(context.Context, netip.AddrPort) (net.Conn, error)
}

func (a TCPAdmission) Authorize(ctx context.Context, peer NodeID, request OpenRequest) (io.Closer, error) {
	if request.Network != 1 {
		return nil, newOpenRejection(OpenProtocolError)
	}
	decision, err := a.Policy.Authorize(ctx, peer, request)
	if err != nil {
		return nil, err
	}
	endpoint, err := ResolveLocalTarget(ctx, decision.Target, a.Resolver)
	if err != nil {
		return nil, newOpenRejection(OpenTargetUnreachable)
	}
	dial := a.Dial
	if dial == nil {
		dial = func(ctx context.Context, endpoint netip.AddrPort) (net.Conn, error) {
			return DialLocalTarget(ctx, "tcp", endpoint)
		}
	}
	conn, err := dial(ctx, endpoint)
	if err != nil {
		return nil, newOpenRejection(OpenTargetUnreachable)
	}
	resource := &TargetResource{Conn: conn}
	if a.Gate != nil {
		resource.Policy, err = a.Gate.Open(ctx, accounting.Flow{Key: peer.String() + "/" + fmt.Sprintf("%x", request.RuleID)})
		if err != nil {
			_ = resource.Close()
			if errors.Is(err, accounting.ErrDenied) {
				return nil, newOpenRejection(OpenQuotaExhausted)
			}
			return nil, newOpenRejection(OpenUnauthorized)
		}
		if resource.Policy == nil {
			_ = resource.Close()
			return nil, newOpenRejection(OpenUnauthorized)
		}
	}
	return resource, nil
}

func validDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}
	name := strings.TrimSuffix(host, ".")
	if name == "" {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
