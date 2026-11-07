package rule

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"
	"vortex/engine/accounting"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

func TestApplyMultihopForwardAndReverseRealTLS(t *testing.T) {
	for _, direction := range []string{"forward", "reverse"} {
		t.Run(direction, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rootPub, rootKey, _ := ed25519.GenerateKey(rand.Reader)
			caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
			caDER, e := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, rootPub, rootKey)
			if e != nil {
				t.Fatal(e)
			}
			ca, _ := x509.ParseCertificate(caDER)
			roots := x509.NewCertPool()
			roots.AddCert(ca)
			ids := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
			boots := []string{"00000000-0000-4000-8000-000000000011", "00000000-0000-4000-8000-000000000012", "00000000-0000-4000-8000-000000000013"}
			managers := make([]*Manager, 3)
			listeners := make([]net.Listener, 3)
			certs := make([]tls.Certificate, 3)
			signers := make([]ed25519.PrivateKey, 3)
			gates := make([]*testGate, 3)
			for i := range ids {
				pub, key, _ := ed25519.GenerateKey(rand.Reader)
				uri, _ := url.Parse("urn:vortex:node:" + ids[i])
				leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), DNSNames: []string{ids[i]}, URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
				der, e := x509.CreateCertificate(rand.Reader, leaf, ca, pub, rootKey)
				if e != nil {
					t.Fatal(e)
				}
				certs[i] = tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key}
				signers[i] = key
				listeners[i], e = net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { listeners[i].Close() })
				gates[i] = &testGate{}
				managers[i] = New(func(Artifact) (accounting.Gate, error) { return gates[i], nil })
			}
			for i := range ids {
				node, _ := vlink.ParseNodeID(ids[i])
				boot, _ := vlink.ParseNodeID(boots[i])
				tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certs[i]}, RootCAs: roots, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}
				if e = managers[i].SetLinkFactory(func(a Artifact, peer Member) (*link.Supervisor, error) {
					peerID, _ := vlink.ParseNodeID(peer.ID)
					return &link.Supervisor{NodeID: node, Signer: signers[i], TLS: func() *tls.Config { c := tlsConfig.Clone(); c.ServerName = peer.ID; return c }(), BootID: vlink.BootID(boot), Generation: uint64(a.PathGeneration), PeerAddr: net.JoinHostPort(peer.PublicAddress, strconv.Itoa(int(peer.LinkPort))), ExpectedPeer: peerID}, nil
				}); e != nil {
					t.Fatal(e)
				}
				go func(i int) {
					for {
						raw, e := listeners[i].Accept()
						if e != nil {
							return
						}
						go func() {
							mux, peer, e := vlink.NewAuthenticatedMuxLink(ctx, tls.Server(raw, tlsConfig), vlink.HelloConfig{NodeID: node, BootID: vlink.BootID(boot), Generation: 1, Capabilities: []uint16{1, 2}, Signer: signers[i], VerifyPeer: func(context.Context, *x509.Certificate, vlink.HelloMessage) error { return nil }}, vlink.MuxConfig{})
							if e != nil {
								raw.Close()
								return
							}
							defer mux.Close(nil)
							managers[i].ServeLink(ctx, mux, peer)
						}()
					}
				}(i)
			}
			target := echoTarget(t, "z")
			udpTarget, e := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(localIP(t))})
			if e != nil {
				t.Fatal(e)
			}
			defer udpTarget.Close()
			go func() {
				b := make([]byte, 65535)
				for {
					n, peer, e := udpTarget.ReadFromUDP(b)
					if e != nil {
						return
					}
					udpTarget.WriteToUDP(append([]byte("u"), b[:n]...), peer)
				}
			}()
			base := artifact(t, target)
			base.RuleID = "00000000-0000-4000-8000-000000000099"
			base.Intent.Rule.ID = base.RuleID
			base.Intent.Rule.Mode = "multi_hop"
			base.Intent.Rule.Path = []string{"entry", "relay", "exit"}
			base.Intent.Segments = []Segment{{Hop: 0, Direction: direction}, {Hop: 1, Direction: "forward"}}
			artifacts := make([]Artifact, 3)
			member := func(i int) Member {
				return Member{ID: ids[i], BootID: boots[i], PublicAddress: "127.0.0.1", LinkPort: int32(listeners[i].Addr().(*net.TCPAddr).Port), Weight: 1, Online: true}
			}
			for i := 2; i >= 0; i-- {
				a := base
				a.NodeID = ids[i]
				a.BootID = boots[i]
				a.Intent.Hop = i
				a.Intent.Role = []string{"entry", "relay", "exit"}[i]
				if i > 0 {
					a.Intent.AllowedUpstream = []string{ids[i-1]}
					a.Intent.UpstreamMembers = []Member{member(i - 1)}
				}
				if i < 2 {
					a.Intent.NextMembers = []Member{member(i + 1)}
				}
				artifacts[i] = a
				if e = managers[i].Prepare([16]byte{1}, a); e != nil {
					t.Fatal(e)
				}
				if e = managers[i].Commit([16]byte{1}); e != nil {
					t.Fatal(e)
				}
			}
			if direction == "reverse" {
				deadline := time.Now().Add(3 * time.Second)
				for {
					managers[0].mu.Lock()
					count := len(managers[0].links[ids[1]])
					managers[0].mu.Unlock()
					if count > 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("reverse handshake absent")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			c := dialRule(t, artifacts[0])
			roundtrip(t, c, "zx")
			c.Close()
			// A second independent rule carries UDP over the same authenticated
			// topology. Datagrams retain boundaries and every hop reserves usage.
			for i := 2; i >= 0; i-- {
				a := artifacts[i]
				a.RuleID = "00000000-0000-4000-8000-000000000098"
				a.Intent.Rule.ID = a.RuleID
				a.Intent.Rule.Protocol = "udp"
				a.Intent.Rule.Port = freePort(t)
				a.Intent.Rule.Targets = []Target{{Address: udpTarget.LocalAddr().String(), Weight: 1}}
				artifacts[i] = a
				if e = managers[i].Prepare([16]byte{2}, a); e != nil {
					t.Fatal(e)
				}
				if e = managers[i].Commit([16]byte{2}); e != nil {
					t.Fatal(e)
				}
			}
			udp, e := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(artifacts[0].Intent.Rule.Port)})
			if e != nil {
				t.Fatal(e)
			}
			udp.SetDeadline(time.Now().Add(3 * time.Second))
			if _, e = udp.Write([]byte("datagram")); e != nil {
				t.Fatal(e)
			}
			b := make([]byte, 64)
			n, e := udp.Read(b)
			udp.Close()
			if e != nil || string(b[:n]) != "udatagram" {
				t.Fatal("UDP multihop", string(b[:n]), e)
			}
			for i := range managers {
				stop, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
				e = managers[i].Stop(stop)
				stopCancel()
				if e != nil {
					t.Error("runtime did not join", i, e)
				}
				if gates[i].opens.Load() == 0 || gates[i].bytes.Load() == 0 {
					t.Error("hop bypassed gate", i)
				}
			}
		})
	}
}
