package httpapi_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vortex/backend/internal/httpapi"
)

func TestPublicTLSListenerBearerBoundaryAndShutdown(t *testing.T) {
	f := newAPI(t)
	// The standard test certificate is trusted explicitly, without skipping verification.
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := fixture.TLS.Certificates[0]
	fixture.Close()
	server, e := httpapi.NewServer(f.handler, certificate)
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- httpapi.Serve(ctx, listener, server) }()
	leaf, e := x509.ParseCertificate(certificate.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	url := "https://" + listener.Addr().String() + "/api/v1/tenants/" + f.tenant + "/operations"
	for _, token := range []string{"", f.pair.RefreshToken, f.pair.AccessToken} {
		request, _ := http.NewRequest("GET", url, nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		expected := 401
		if token == f.pair.AccessToken {
			expected = 200
		}
		if response.StatusCode != expected || response.TLS.Version != tls.VersionTLS13 || strings.Contains(string(body), "PRIVATE") {
			t.Fatal(response.StatusCode, string(body))
		}
	}
	// A supplied client certificate still cannot replace a Bearer principal.
	withCertificate := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}}, Timeout: 3 * time.Second}
	response, e := withCertificate.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	withCertificate.CloseIdleConnections()
	if response.StatusCode != 401 {
		t.Fatal("client certificate became API authority", response.StatusCode)
	}
	oldTLS := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MaxVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	if response, e := oldTLS.Get(url); e == nil {
		response.Body.Close()
		t.Fatal("TLS1.2 accepted")
	}
	oldTLS.CloseIdleConnections()
	streamRequest, _ := http.NewRequest("GET", "https://"+listener.Addr().String()+"/api/v1/tenants/"+f.tenant+"/events", nil)
	streamRequest.Header.Set("Authorization", "Bearer "+f.pair.AccessToken)
	stream, e := client.Do(streamRequest)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if _, e = io.ReadAll(stream.Body); e != nil {
		t.Fatal("active event stream failed to close gracefully", e)
	}
	stream.Body.Close()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("public server did not shut down")
	}
}
