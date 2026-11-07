package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type dnsRoundTrip func(*http.Request) (*http.Response, error)

func (f dnsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func dnsResponse(status int, value any) *http.Response {
	raw, _ := json.Marshal(map[string]any{"success": status == 200, "result": value, "errors": []any{}})
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}}
}
func dnsPolicy(id, zone string) []byte {
	b, _ := json.Marshal(map[string]any{"id": id, "status": "active", "zone_name": "example.test", "policies": []any{map[string]any{"effect": "allow", "resources": map[string]string{"com.cloudflare.api.account.zone." + zone: "*"}, "permission_groups": []any{map[string]string{"id": "4755a26eedb94da69e1066d98aa820be", "name": "DNS Write"}}}}})
	return b
}
func TestDNSCredentialPolicyIdentityAndExactZone(t *testing.T) {
	zone, id := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, tc := range []struct {
		name   string
		policy []byte
		want   bool
	}{{"dedicated", dnsPolicy(id, zone), true}, {"different token", dnsPolicy(strings.Repeat("c", 32), zone), false}, {"other zone", dnsPolicy(id, strings.Repeat("c", 32)), false}, {"wildcard", []byte(strings.ReplaceAll(string(dnsPolicy(id, zone)), "com.cloudflare.api.account.zone."+zone, "com.cloudflare.api.account.zone.*")), false}, {"extra permission", []byte(strings.ReplaceAll(string(dnsPolicy(id, zone)), "4755a26eedb94da69e1066d98aa820be", "ffffffffffffffffffffffffffffffff")), false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCloudflareWithPolicyEvidence(tc.policy)
			calls := 0
			c.client.Transport = dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.cloudflare.com" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					t.Fatal("credential endpoint")
				}
				if strings.HasSuffix(r.URL.Path, "verify") {
					return dnsResponse(200, map[string]string{"id": id, "status": "active"}), nil
				}
				return dnsResponse(200, []DNSRecord{}), nil
			})
			_, e := c.VerifyZone(context.Background(), []byte("test-token-value-only"), zone)
			if (e == nil) != tc.want {
				t.Fatal("policy accepted incorrectly", e)
			}
			if !tc.want && calls != 1 {
				t.Fatal("policy rejection made zone request")
			}
		})
	}
}
func TestDNSBoundedRetryRedactionAndNoBlindCreate(t *testing.T) {
	c := NewCloudflare(nil)
	ctx := context.Background()
	zone := strings.Repeat("a", 32)
	calls := 0
	c.client.Transport = dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return dnsResponse(503, map[string]string{"secret": "should never escape"}), nil
	})
	if _, e := c.List(ctx, []byte("test-token-value-only"), zone, "test.example.test"); e != ErrDNS || calls != 3 {
		t.Fatal("bounded read retries", e, calls)
	}
	calls = 0
	r := DNSRecord{Type: "A", Name: "test.example.test", Content: "192.0.2.1", TTL: 60, Comment: "vortex:owner"}
	if _, e := c.Create(ctx, []byte("test-token-value-only"), zone, r); e != ErrDNS || calls != 1 {
		t.Fatal("ambiguous POST blindly retried", e, calls)
	}
	calls = 0
	r.Proxied = true
	if _, e := c.Create(ctx, []byte("test-token-value-only"), zone, r); e == nil || calls != 0 {
		t.Fatal("proxied request sent")
	}
	c.client.Transport = dnsRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 1<<20+1)))}, nil
	})
	if _, e := c.List(ctx, []byte("test-token-value-only"), zone, "test.example.test"); e != ErrDNS {
		t.Fatal("oversized response accepted")
	}
}
func TestDNSReconcileIdempotentOwnershipAndCleanup(t *testing.T) {
	c := NewCloudflare(nil)
	zone := strings.Repeat("a", 32)
	token := []byte("test-token-value-only")
	name := "test.example.test"
	records := []DNSRecord{}
	writes := 0
	c.client.Transport = dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return dnsResponse(200, records), nil
		}
		writes++
		if r.Method == "DELETE" {
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			for i, row := range records {
				if row.ID == id {
					records = append(records[:i], records[i+1:]...)
					break
				}
			}
			return dnsResponse(200, map[string]string{"id": id}), nil
		}
		var row DNSRecord
		if json.NewDecoder(r.Body).Decode(&row) != nil {
			t.Fatal("body")
		}
		if row.TTL != 60 || row.Proxied {
			t.Fatal("DNS defaults")
		}
		row.ID = strings.Repeat("b", 32)
		records = append(records, row)
		return dnsResponse(200, row), nil
	})
	for i := 0; i < 2; i++ {
		if e := c.Reconcile(context.Background(), token, zone, name, []string{"192.0.2.1"}, "owner"); e != nil {
			t.Fatal(e)
		}
	}
	if writes != 1 {
		t.Fatal("replay duplicated create", writes)
	}
	if e := c.Reconcile(context.Background(), token, zone, name, nil, "other-owner"); e == nil || writes != 1 {
		t.Fatal("foreign owner changed")
	}
	if e := c.Reconcile(context.Background(), token, zone, name, nil, "owner"); e != nil || len(records) != 0 || writes != 2 {
		t.Fatal("cleanup", e)
	}
}

func TestDNSUpdateAndMissingProxyResponseFailClosed(t *testing.T) {
	c := NewCloudflare(nil)
	zone := strings.Repeat("a", 32)
	id := strings.Repeat("b", 32)
	r := DNSRecord{ID: id, Type: "A", Name: "test.example.test", Content: "192.0.2.2", TTL: 60, Comment: "vortex:owner"}
	c.client.Transport = dnsRoundTrip(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			t.Fatal("invalid update JSON")
		}
		if _, present := body["id"]; present {
			t.Fatal("record identifier sent in update body")
		}
		return dnsResponse(200, r), nil
	})
	if _, e := c.Update(context.Background(), []byte("test-token-value-only"), zone, r); e != nil {
		t.Fatal(e)
	}
	c.client.Transport = dnsRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"errors":[],"result":{"id":"` + id + `","type":"A","name":"test.example.test","content":"192.0.2.2","ttl":60,"comment":"vortex:owner"}}`))}, nil
	})
	if _, e := c.Read(context.Background(), []byte("test-token-value-only"), zone, id); e != ErrDNS {
		t.Fatal("missing proxy confirmation accepted", e)
	}
}
