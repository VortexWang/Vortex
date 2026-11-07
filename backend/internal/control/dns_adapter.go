package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrDNS = errors.New("DNS provider request failed or response mismatch")
var cfID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type DNSZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func (r *DNSRecord) UnmarshalJSON(raw []byte) error {
	type record DNSRecord
	var decoded record
	if json.Unmarshal(raw, &decoded) != nil {
		return ErrDNS
	}
	if decoded.Type == "A" || decoded.Type == "AAAA" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || !bytes.Equal(fields["proxied"], []byte("false")) && !bytes.Equal(fields["proxied"], []byte("true")) {
			return ErrDNS
		}
	}
	*r = DNSRecord(decoded)
	return nil
}

type DNSProvider interface {
	VerifyZone(context.Context, []byte, string) (DNSZone, error)
	Reconcile(context.Context, []byte, string, string, []string, string) error
}

// No caller-selected endpoint or redirect can receive a production token.
// policyToken is an optional independent User API Tokens Read credential; the
// DNS token itself remains restricted to DNS Write in exactly one zone.
type Cloudflare struct {
	client         *http.Client
	policyToken    []byte
	policyEvidence []byte
}

func NewCloudflare(policyToken []byte) *Cloudflare {
	return &Cloudflare{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDNS }}, policyToken: append([]byte(nil), policyToken...)}
}

// This evidence is trusted deployment configuration exported/reviewed at token
// creation, never an HTTP client body. VerifyZone binds its ID to Cloudflare's
// authenticated verify response. It avoids granting token-management rights to
// the dedicated DNS token. The JSON contains no token value.
func NewCloudflareWithPolicyEvidence(evidence []byte) *Cloudflare {
	c := NewCloudflare(nil)
	c.policyEvidence = append([]byte(nil), evidence...)
	return c
}
func validToken(token []byte) bool {
	return len(token) >= 16 && len(token) <= 4096 && !strings.ContainsAny(string(token), "\r\n\t ")
}

func (c *Cloudflare) request(ctx context.Context, token []byte, method, path string, body any, out any) error {
	if c == nil || c.client == nil || !validToken(token) {
		return ErrCredential
	}
	var data []byte
	if body != nil {
		var e error
		data, e = json.Marshal(body)
		if e != nil {
			return ErrDNS
		}
	}
	attempts := 1
	if method == http.MethodGet || method == http.MethodDelete {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ErrDNS
			case <-timer.C:
			}
		}
		req, e := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, bytes.NewReader(data))
		if e != nil {
			return ErrDNS
		}
		req.Header.Set("Authorization", "Bearer "+string(token))
		req.Header.Set("Content-Type", "application/json")
		response, e := c.client.Do(req)
		if e != nil {
			if method == http.MethodGet {
				continue
			}
			return ErrDNS
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
		response.Body.Close()
		if e != nil || len(raw) > 1<<20 {
			return ErrDNS
		}
		if (response.StatusCode == 429 || response.StatusCode >= 500) && attempt+1 < attempts {
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return ErrDNS
		}
		var envelope struct {
			Success bool              `json:"success"`
			Result  json.RawMessage   `json:"result"`
			Errors  []json.RawMessage `json:"errors"`
			Info    struct {
				TotalCount int `json:"total_count"`
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if json.Unmarshal(raw, &envelope) != nil || !envelope.Success || len(envelope.Errors) > 0 || len(envelope.Result) == 0 {
			return ErrDNS
		}
		if envelope.Info.TotalPages > 1 || envelope.Info.TotalCount > 100 {
			return ErrDNS
		}
		if out != nil && json.Unmarshal(envelope.Result, out) != nil {
			return ErrDNS
		}
		return nil
	}
	return ErrDNS
}

// Token verify proves activity only. Inspect the actual policy before using the
// credential: exactly DNS Write, one explicit zone, and no wildcard/account/user
// permissions. The optional inspector credential is never persisted or audited.
func (c *Cloudflare) VerifyZone(ctx context.Context, token []byte, zone string) (DNSZone, error) {
	if !cfID.MatchString(zone) {
		return DNSZone{}, ErrCredential
	}
	var verified struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if e := c.request(ctx, token, http.MethodGet, "/user/tokens/verify", nil, &verified); e != nil || verified.Status != "active" || !cfID.MatchString(verified.ID) {
		return DNSZone{}, ErrCredential
	}
	inspector := c.policyToken
	if len(inspector) == 0 {
		inspector = token
	}
	var details struct {
		ZoneName string `json:"zone_name"`
		ID       string `json:"id"`
		Status   string `json:"status"`
		Policies []struct {
			Effect      string            `json:"effect"`
			Resources   map[string]string `json:"resources"`
			Permissions []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"permission_groups"`
		} `json:"policies"`
	}
	var policyError error
	if len(c.policyEvidence) > 0 {
		policyError = json.Unmarshal(c.policyEvidence, &details)
	} else {
		policyError = c.request(ctx, inspector, http.MethodGet, "/user/tokens/"+verified.ID, nil, &details)
	}
	if policyError != nil || details.ID != verified.ID || details.Status != "active" || len(details.Policies) != 1 {
		return DNSZone{}, ErrCredential
	}
	p := details.Policies[0]
	// Cloudflare's stable DNS Write permission-group ID; the dashboard labels it DNS Edit.
	if p.Effect != "allow" || len(p.Resources) != 1 || p.Resources["com.cloudflare.api.account.zone."+zone] != "*" || len(p.Permissions) != 1 || p.Permissions[0].ID != "4755a26eedb94da69e1066d98aa820be" {
		return DNSZone{}, ErrCredential
	}
	if len(c.policyEvidence) > 0 {
		if !dnsName.MatchString(details.ZoneName) || len(details.ZoneName) > 253 {
			return DNSZone{}, ErrCredential
		}
		if _, e := c.List(ctx, token, zone, "vortex-scope-check."+details.ZoneName); e != nil {
			return DNSZone{}, ErrCredential
		}
		return DNSZone{ID: zone, Name: details.ZoneName}, nil
	}
	// The optional metadata-inspector route is useful for automated enrollment.
	var result DNSZone
	if e := c.request(ctx, inspector, http.MethodGet, "/zones/"+zone, nil, &result); e != nil || result.ID != zone || !dnsName.MatchString(result.Name) {
		return DNSZone{}, ErrCredential
	}
	return result, nil
}

func validateRecord(r DNSRecord) error {
	ip, e := netip.ParseAddr(r.Content)
	if e != nil || ip.String() != r.Content || r.TTL != 60 || r.Proxied || !dnsName.MatchString(r.Name) || len(r.Name) > 253 || len(r.Comment) > 128 || !strings.HasPrefix(r.Comment, "vortex:") {
		return ErrDNS
	}
	if (r.Type == "A" && !ip.Is4()) || (r.Type == "AAAA" && (!ip.Is6() || ip.Is4In6())) || (r.Type != "A" && r.Type != "AAAA") {
		return ErrDNS
	}
	return nil
}
func recordMatches(a, b DNSRecord) bool {
	return a.Type == b.Type && a.Name == b.Name && a.Content == b.Content && a.TTL == b.TTL && a.Proxied == b.Proxied && a.Comment == b.Comment
}
func (c *Cloudflare) List(ctx context.Context, token []byte, zone, name string) ([]DNSRecord, error) {
	if !cfID.MatchString(zone) || !dnsName.MatchString(name) {
		return nil, ErrDNS
	}
	var rows []DNSRecord
	if e := c.request(ctx, token, http.MethodGet, "/zones/"+zone+"/dns_records?per_page=100&name="+url.QueryEscape(name), nil, &rows); e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, ErrDNS
	}
	for _, r := range rows {
		if r.Name != name || !cfID.MatchString(r.ID) {
			return nil, ErrDNS
		}
	}
	return rows, nil
}
func (c *Cloudflare) Create(ctx context.Context, token []byte, zone string, record DNSRecord) (DNSRecord, error) {
	if !cfID.MatchString(zone) || validateRecord(record) != nil || record.ID != "" {
		return DNSRecord{}, ErrDNS
	}
	var result DNSRecord
	e := c.request(ctx, token, http.MethodPost, "/zones/"+zone+"/dns_records", record, &result)
	if e != nil || !cfID.MatchString(result.ID) || !recordMatches(result, record) {
		return DNSRecord{}, ErrDNS
	}
	return result, nil
}
func (c *Cloudflare) Read(ctx context.Context, token []byte, zone, id string) (DNSRecord, error) {
	if !cfID.MatchString(zone) || !cfID.MatchString(id) {
		return DNSRecord{}, ErrDNS
	}
	var result DNSRecord
	e := c.request(ctx, token, http.MethodGet, "/zones/"+zone+"/dns_records/"+id, nil, &result)
	if e != nil || result.ID != id || validateRecord(result) != nil {
		return DNSRecord{}, ErrDNS
	}
	return result, nil
}
func (c *Cloudflare) Update(ctx context.Context, token []byte, zone string, record DNSRecord) (DNSRecord, error) {
	if !cfID.MatchString(zone) || !cfID.MatchString(record.ID) || validateRecord(record) != nil {
		return DNSRecord{}, ErrDNS
	}
	var result DNSRecord
	e := c.request(ctx, token, http.MethodPut, "/zones/"+zone+"/dns_records/"+record.ID, record, &result)
	if e != nil || result.ID != record.ID || !recordMatches(result, record) {
		return DNSRecord{}, ErrDNS
	}
	return result, nil
}
func (c *Cloudflare) Delete(ctx context.Context, token []byte, zone, id string) error {
	if !cfID.MatchString(zone) || !cfID.MatchString(id) {
		return ErrDNS
	}
	var result struct {
		ID string `json:"id"`
	}
	if e := c.request(ctx, token, http.MethodDelete, "/zones/"+zone+"/dns_records/"+id, nil, &result); e != nil || result.ID != id {
		return ErrDNS
	}
	return nil
}
func (c *Cloudflare) Reconcile(ctx context.Context, token []byte, zone, name string, addresses []string, owner string) error {
	if len(addresses) > 32 || owner == "" || len(owner) > 100 {
		return ErrDNS
	}
	desired := map[string]DNSRecord{}
	for _, address := range addresses {
		ip, e := netip.ParseAddr(address)
		if e != nil {
			return ErrDNS
		}
		kind := "AAAA"
		if ip.Is4() {
			kind = "A"
		}
		r := DNSRecord{Type: kind, Name: name, Content: address, TTL: 60, Comment: "vortex:" + owner}
		if validateRecord(r) != nil {
			return ErrDNS
		}
		desired[address] = r
	}
	existing, e := c.List(ctx, token, zone, name)
	if e != nil {
		return e
	}
	// Fail before any write if another owner has records at this exact name.
	for _, r := range existing {
		if r.Comment != "vortex:"+owner || (r.Type != "A" && r.Type != "AAAA") {
			return ErrDNS
		}
	}
	found := map[string]bool{}
	for _, r := range existing {
		d, keep := desired[r.Content]
		if !keep || found[r.Content] {
			continue
		}
		found[r.Content] = true
		if !recordMatches(r, d) {
			d.ID = r.ID
			if _, e = c.Update(ctx, token, zone, d); e != nil {
				return e
			}
		}
	}
	for address, r := range desired {
		if !found[address] {
			if _, e = c.Create(ctx, token, zone, r); e != nil {
				return e
			}
		}
	}
	// Removal affects DNS only, never port occupation or execution receipts.
	retained := map[string]bool{}
	for _, r := range existing {
		_, keep := desired[r.Content]
		if keep && !retained[r.Content] {
			retained[r.Content] = true
			continue
		}
		if e = c.Delete(ctx, token, zone, r.ID); e != nil {
			return e
		}
	}
	actual, e := c.List(ctx, token, zone, name)
	if e != nil || len(actual) != len(desired) {
		return ErrDNS
	}
	seen := map[string]bool{}
	for _, r := range actual {
		d, ok := desired[r.Content]
		if !ok || seen[r.Content] || !recordMatches(r, d) {
			return ErrDNS
		}
		seen[r.Content] = true
	}
	return nil
}
