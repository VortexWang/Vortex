package httpapi

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"
)

// NewServer is the public Bearer listener. Control-plane trust roots and client
// certificates are deliberately absent from its authentication configuration.
func NewServer(handler http.Handler, certificate tls.Certificate) (*http.Server, error) {
	if handler == nil || len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return nil, errors.New("public API handler and server certificate required")
	}
	return &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024,
		// No global write deadline: SSE uses bounded per-write deadlines.
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ClientAuth: tls.NoClientCert},
	}, nil
}

func Serve(ctx context.Context, listener net.Listener, server *http.Server) error {
	if ctx == nil || listener == nil || server == nil || server.TLSConfig == nil {
		return errors.New("incomplete public API server")
	}
	if server.BaseContext == nil {
		server.BaseContext = func(net.Listener) context.Context { return ctx }
	}
	defer server.Close()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	err := server.ServeTLS(listener, "", "")
	close(done)
	<-finished
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
