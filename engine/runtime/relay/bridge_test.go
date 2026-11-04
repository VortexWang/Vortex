package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"vortex/engine/accounting"
	memory "vortex/engine/runtime/accounting"
	"vortex/engine/vlink"
)

// Exercise the runtime bridge, real authenticated VLink and evidence adapter,
// rather than the older library-only UDPResource path.
func TestRuntimeAmbiguousTargetWriteRetainsUnknownAfterShutdown(t *testing.T) {
	for _, network := range []vlink.StreamNetwork{vlink.StreamTCP, vlink.StreamUDP} {
		for _, writeErr := range []error{nil, io.ErrUnexpectedEOF} {
			t.Run(string(rune('0'+network))+"/"+fmtError(writeErr), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				client, server := authenticatedPair(t, ctx)
				defer client.Close(nil)
				defer server.Close(nil)
				out, err := client.Open(ctx, vlink.OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: uint8(network), ClientAddress: []byte{127, 0, 0, 1}})
				if err != nil {
					t.Fatal(err)
				}
				in, err := server.Accept(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := server.AuthorizeRemote(ctx, in.ID, func(context.Context, vlink.NodeID, vlink.OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
					t.Fatal(err)
				}
				if err := out.WaitOpen(ctx); err != nil {
					t.Fatal(err)
				}
				gate, _ := memory.NewGate(8)
				target := &shortTarget{done: make(chan struct{}), err: writeErr}
				finished := make(chan error, 1)
				go func() { finished <- BridgeConnWithGate(ctx, in, target, gate) }()
				payload := []byte("ambiguous-target-write")
				if network == vlink.StreamTCP {
					err = out.WriteData(ctx, payload)
				} else {
					var accepted bool
					accepted, err = out.WriteDgram(ctx, payload)
					if !accepted {
						t.Fatalf("not admitted: %v", err)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					if err == nil {
						t.Fatal("short write succeeded")
					}
				case <-ctx.Done():
					t.Fatal("runtime bridge did not stop")
				}
				client.Close(io.EOF)
				server.Close(io.EOF)
				var evidence bytes.Buffer
				if err := gate.WriteJSONL(&evidence); err != nil {
					t.Fatal(err)
				}
				var event memory.Event
				decoder := json.NewDecoder(&evidence)
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event.Outcome != accounting.Unknown || event.Reserved != int64(len(payload)) || event.Used != 0 || event.Direction != accounting.LeftToRight || event.Flow.Key != in.FlowKey() {
					t.Fatalf("lost unknown upper bound: %+v", event)
				}
				if err := decoder.Decode(&event); !errors.Is(err, io.EOF) {
					t.Fatalf("shutdown resolved reservation twice: %v", err)
				}
			})
		}
	}
}
func fmtError(err error) string {
	if err == nil {
		return "short-nil"
	}
	return "short-error"
}

type shortTarget struct {
	net.Conn
	done chan struct{}
	once sync.Once
	err  error
}

func (c *shortTarget) Write(p []byte) (int, error)     { return len(p) / 2, c.err }
func (c *shortTarget) Read([]byte) (int, error)        { <-c.done; return 0, net.ErrClosed }
func (c *shortTarget) Close() error                    { c.once.Do(func() { close(c.done) }); return nil }
func (c *shortTarget) SetReadDeadline(time.Time) error { return nil }

func authenticatedPair(t *testing.T, ctx context.Context) (*vlink.MuxLink, *vlink.MuxLink) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	type identity struct {
		cfg   *tls.Config
		hello vlink.HelloConfig
	}
	makeID := func(n byte, client bool) identity {
		id := vlink.NodeID{n}
		u, _ := url.Parse("urn:vortex:node:" + id.String())
		pub, signer, _ := ed25519.GenerateKey(rand.Reader)
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(n + 1)), URIs: []*url.URL{u}, DNSNames: []string{"vortex"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, pub, key)
		if err != nil {
			t.Fatal(err)
		}
		return identity{&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: signer}}, RootCAs: roots, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, ServerName: "vortex"}, vlink.HelloConfig{Client: client, NodeID: id, BootID: vlink.BootID{n}, Generation: 1, Signer: signer, VerifyPeer: func(context.Context, *x509.Certificate, vlink.HelloMessage) error { return nil }, Capabilities: []uint16{1, 2}}}
	}
	a, b := makeID(1, true), makeID(2, false)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type result struct {
		mux *vlink.MuxLink
		err error
	}
	ready := make(chan result, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			ready <- result{err: e}
			return
		}
		mux, _, e := vlink.NewAuthenticatedMuxLink(ctx, tls.Server(conn, b.cfg), b.hello, vlink.MuxConfig{})
		ready <- result{mux, e}
	}()
	conn, err := (&tls.Dialer{Config: a.cfg}).DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	left, _, err := vlink.NewAuthenticatedMuxLink(ctx, conn.(*tls.Conn), a.hello, vlink.MuxConfig{LocalClient: true})
	if err != nil {
		t.Fatal(err)
	}
	r := <-ready
	if r.err != nil {
		left.Close(nil)
		t.Fatal(r.err)
	}
	return left, r.mux
}
