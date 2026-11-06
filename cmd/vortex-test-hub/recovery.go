package main

// Everything in this file is an opt-in protocol-test backend adapter. Its
// offline request is a harness provision, never a production authority source.
import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
	"vortex/agent/store"
	"vortex/hub"
)

type testRecoveryBackend struct {
	client   *http.Client
	endpoint string
}

func (b *testRecoveryBackend) AcceptLedger(ctx context.Context, r store.LedgerRecord) ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	res, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("test backend: receipt unavailable")
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, 33))
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("test backend: invalid receipt")
	}
	return raw, nil
}
func runTestRecovery(ctx context.Context, s *store.Store, id, request, endpoint, backendName string, cert tls.Certificate, pool *x509.CertPool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		panic("test recovery requires HTTPS backend")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: backendName}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	backend := &testRecoveryBackend{client: &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: endpoint}
	issuer := &hub.Issuer{Store: s, HubID: id, Clock: func() store.IssuerTime {
		now := time.Now().UTC()
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			raw, err := os.ReadFile(request)
			if err != nil {
				continue
			}
			var h store.HandoffRequest
			if len(raw) > 16384 || json.Unmarshal(raw, &h) != nil || h.HubID != id {
				continue
			}
			if _, err = issuer.Recover(ctx, h, backend); err != nil {
				continue
			}
		}
	}
}
