package vlink

import (
	"context"
	"net/netip"
	"testing"
)

type fixedTargetResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
}

func (r *fixedTargetResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	return r.addresses, r.err
}

func TestResolveLocalTargetReturnsNumericAllowlistedEndpoint(t *testing.T) {
	resolver := &fixedTargetResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	endpoint, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: "service.example", Port: 443, AllowedPrefixes: []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")}}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != netip.MustParseAddrPort("8.8.8.8:443") || resolver.calls != 1 {
		t.Fatalf("endpoint=%v resolver calls=%d", endpoint, resolver.calls)
	}
}

func TestResolveLocalTargetRejectsPrivateReservedAndMalformedAddresses(t *testing.T) {
	for _, host := range []string{"10.1.2.3", "127.0.0.1", "169.254.2.1", "192.168.1.4", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "255.1.1.1", "::1", "fc00::1", "fe80::1", "2001:db8::1"} {
		t.Run(host, func(t *testing.T) {
			if _, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: host, Port: 80}, nil); err == nil {
				t.Fatalf("accepted forbidden address %s", host)
			}
		})
	}
	for _, host := range []string{"bad host", "-bad.example", "bad..example", "x/../y", ""} {
		if _, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: host, Port: 80}, nil); err == nil {
			t.Fatalf("accepted malformed host %q", host)
		}
	}
}

func TestResolveLocalTargetRejectsDNSRebindingAnswerSet(t *testing.T) {
	resolver := &fixedTargetResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("192.168.0.1")}}
	if _, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: "rebound.example", Port: 443}, resolver); err == nil {
		t.Fatal("accepted mixed public/private DNS answer")
	}
	if resolver.calls != 1 {
		t.Fatalf("DNS lookup count=%d", resolver.calls)
	}
}

func TestResolveLocalTargetRejectsEmptyDNSAndPrefixMismatch(t *testing.T) {
	if _, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: "missing.example", Port: 443}, &fixedTargetResolver{}); err == nil {
		t.Fatal("accepted empty DNS result")
	}
	resolver := &fixedTargetResolver{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}
	if _, err := ResolveLocalTarget(context.Background(), LocalTarget{Host: "service.example", Port: 443, AllowedPrefixes: []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")}}, resolver); err == nil {
		t.Fatal("accepted target outside local allowlist")
	}
}

func TestDialLocalTargetRejectsUnsafeEndpointWithoutDetails(t *testing.T) {
	_, err := DialLocalTarget(context.Background(), "tcp", netip.MustParseAddrPort("127.0.0.1:80"))
	if err != ErrTargetRejected {
		t.Fatalf("error=%v", err)
	}
	_, err = DialLocalTarget(context.Background(), "udp", netip.MustParseAddrPort("8.8.8.8:0"))
	if err != ErrTargetRejected {
		t.Fatalf("error=%v", err)
	}
}
