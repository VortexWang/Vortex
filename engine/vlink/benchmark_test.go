package vlink

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"testing"
	"time"
)

const perfPayloadSize = 64 * 1024 * 1024

func BenchmarkVLinkTCPThroughput(b *testing.B) {
	clientID, serverID := NodeID{0x31}, NodeID{0x32}
	clientTLS, clientSigner := testTLSIdentity(b, clientID)
	serverTLS, serverSigner := testTLSIdentity(b, serverID)
	trustTLS13Peers(b, clientTLS, serverTLS)
	peerVerifier := func(context.Context, *x509.Certificate, HelloMessage) error { return nil }
	payload := make([]byte, perfPayloadSize)
	for _, direction := range []struct {
		name    string
		reverse bool
	}{{"forward", false}, {"reverse", true}} {
		b.Run("vlink-tracer/"+direction.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			for range b.N {
				if err := runTracerTransfer(b, payload, direction.reverse, clientTLS, serverTLS, clientSigner, serverSigner, clientID, serverID, peerVerifier); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	b.Run("tls-baseline", func(b *testing.B) {
		b.SetBytes(int64(len(payload)))
		for range b.N {
			if err := runTLSBaseline(b, payload, clientTLS, serverTLS); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, direction := range []struct {
		name    string
		reverse bool
	}{{"forward", false}, {"reverse-established", true}} {
		b.Run("vlink-mux/"+direction.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			for range b.N {
				if err := runMuxTransfer(b, payload, direction.reverse, clientTLS, serverTLS, clientSigner, serverSigner, clientID, serverID, peerVerifier); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkVLinkFirstByte(b *testing.B) {
	for _, mode := range []string{"mux", "pool-prewarmed"} {
		b.Run(mode, func(b *testing.B) {
			clientID, serverID := NodeID{0x41}, NodeID{0x42}
			clientTLS, clientSigner := testTLSIdentity(b, clientID)
			serverTLS, serverSigner := testTLSIdentity(b, serverID)
			trustTLS13Peers(b, clientTLS, serverTLS)
			peerVerifier := func(context.Context, *x509.Certificate, HelloMessage) error { return nil }
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			rtts := measureLoopbackRTTs(b, ctx)
			samples := make([]time.Duration, 0, b.N)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				b.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			acceptErr := make(chan error, 1)
			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						acceptErr <- err
						return
					}
					accepted <- conn
				}
			}()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				client, server := benchmarkMuxPair(b, ctx, clientTLS, serverTLS, clientSigner, serverSigner, clientID, serverID, peerVerifier)
				startMuxResponder(ctx, server)
				var pool *LinkPool
				if mode == "pool-prewarmed" {
					var err error
					pool, err = NewLinkPool(PoolConfig{MinLinks: 1, MaxLinks: 1, HardCap: 1}, func(context.Context) (PooledLink, error) {
						return benchmarkPooledLink{mux: client}, nil
					})
					if err != nil {
						b.Fatal(err)
					}
					if err = pool.Scale(ctx, 0, true); err != nil {
						b.Fatal(err)
					}
				}
				started := time.Now()
				link := client
				var release func()
				if pool != nil {
					pooled, done, err := pool.Acquire(ctx, ModePool)
					if err != nil {
						b.Fatal(err)
					}
					link = pooled.(benchmarkPooledLink).mux
					release = done
				}
				tcpClient, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
				if dialErr != nil {
					b.Fatal(dialErr)
				}
				var tcpServer net.Conn
				select {
				case tcpServer = <-accepted:
				case acceptFailure := <-acceptErr:
					b.Fatal(acceptFailure)
				case <-ctx.Done():
					b.Fatal(ctx.Err())
				}
				_, err := measureMuxFirstByte(ctx, link)
				if err != nil {
					b.Fatal(err)
				}
				_ = tcpClient.Close()
				_ = tcpServer.Close()
				if release != nil {
					release()
				}
				if pool != nil {
					_ = pool.Close()
				}
				client.Close(ErrProtocol)
				server.Close(ErrProtocol)
				samples = append(samples, time.Since(started))
			}
			b.StopTimer()
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			median := samples[len(samples)/2]
			baselineRTT := rtts.link + rtts.target
			extra := median - baselineRTT
			b.ReportMetric(float64(median.Microseconds()), "median-first-byte-us")
			b.ReportMetric(float64(baselineRTT.Microseconds()), "median-rtt-sum-us")
			b.ReportMetric(float64(extra.Microseconds()), "median-extra-us")
		})
	}
}

type loopbackRTTs struct{ link, target time.Duration }

func measureLoopbackRTTs(tb testing.TB, _ context.Context) loopbackRTTs {
	measure := func() time.Duration {
		const sampleCount = nineRTTSamples
		samples := make([]time.Duration, 0, sampleCount)
		for range sampleCount {
			client, server := tcpPair(tb)
			serverDone := make(chan error, 1)
			go func() {
				var one [1]byte
				if _, err := io.ReadFull(server, one[:]); err != nil {
					serverDone <- err
					return
				}
				_, err := server.Write(one[:])
				serverDone <- err
			}()
			start := time.Now()
			if _, err := client.Write([]byte{1}); err != nil {
				tb.Fatal(err)
			}
			var one [1]byte
			if _, err := io.ReadFull(client, one[:]); err != nil {
				tb.Fatal(err)
			}
			if err := <-serverDone; err != nil {
				tb.Fatal(err)
			}
			samples = append(samples, time.Since(start))
			_ = client.Close()
			_ = server.Close()
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return samples[len(samples)/2]
	}
	return loopbackRTTs{link: measure(), target: measure()}
}

const nineRTTSamples = 9

type benchmarkPooledLink struct{ mux *MuxLink }

func (l benchmarkPooledLink) Healthy() bool {
	select {
	case <-l.mux.Closed():
		return false
	default:
		return true
	}
}
func (l benchmarkPooledLink) Close() error { l.mux.Close(ErrProtocol); return nil }

func benchmarkMuxPair(tb testing.TB, ctx context.Context, clientTLS, serverTLS *tls.Config, clientSigner, serverSigner crypto.Signer, clientID, serverID NodeID, verify PeerVerifier) (*MuxLink, *MuxLink) {
	return benchmarkMuxPairForDirection(tb, ctx, false, clientTLS, serverTLS, clientSigner, serverSigner, clientID, serverID, verify)
}

func benchmarkMuxPairForDirection(tb testing.TB, ctx context.Context, reverseEstablished bool, clientTLS, serverTLS *tls.Config, clientSigner, serverSigner crypto.Signer, clientID, serverID NodeID, verify PeerVerifier) (*MuxLink, *MuxLink) {
	clientRaw, serverRaw := tcpPair(tb)
	serverDone := make(chan *MuxLink, 1)
	serverErr := make(chan error, 1)
	serverConfig, serverHello := serverTLS, HelloConfig{NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: verify}
	clientConfig, clientHello := clientTLS, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{1}, Generation: 1, Signer: clientSigner, VerifyPeer: verify}
	if reverseEstablished {
		serverConfig, serverHello = clientTLS, HelloConfig{NodeID: clientID, BootID: BootID{1}, Generation: 1, Signer: clientSigner, VerifyPeer: verify}
		clientConfig, clientHello = serverTLS, HelloConfig{Client: true, NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: verify}
	}
	serverTLSConfig := serverConfig.Clone()
	if reverseEstablished {
		cert, err := x509.ParseCertificate(serverTLS.Certificates[0].Certificate[0])
		if err != nil {
			tb.Fatal(err)
		}
		serverTLSConfig.ClientCAs = x509.NewCertPool()
		serverTLSConfig.ClientCAs.AddCert(cert)
		serverTLSConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	serverMuxConfig := MuxConfig{LocalClient: false, Limits: benchmarkMuxLimits()}
	go func() {
		link, _, err := NewAuthenticatedMuxLink(ctx, tls.Server(serverRaw, serverTLSConfig), serverHello, serverMuxConfig)
		if err != nil {
			serverErr <- err
			return
		}
		serverDone <- link
	}()
	clientTLSConfig := clientConfig.Clone()
	if clientTLSConfig.ServerName == "" {
		clientTLSConfig.ServerName = clientTLS.ServerName
	}
	if reverseEstablished {
		cert, err := x509.ParseCertificate(clientTLS.Certificates[0].Certificate[0])
		if err != nil {
			tb.Fatal(err)
		}
		clientTLSConfig.RootCAs = x509.NewCertPool()
		clientTLSConfig.RootCAs.AddCert(cert)
	}
	client, _, err := NewAuthenticatedMuxLink(ctx, tls.Client(clientRaw, clientTLSConfig), clientHello, MuxConfig{LocalClient: true, Limits: benchmarkMuxLimits()})
	if err != nil {
		tb.Fatal(err)
	}
	select {
	case server := <-serverDone:
		return client, server
	case err := <-serverErr:
		tb.Fatal(err)
	case <-ctx.Done():
		tb.Fatal(ctx.Err())
	}
	return nil, nil
}

func benchmarkMuxLimits() LimitConfig {
	limits := defaultFrameLimits()
	limits[FrameWindow] = FrameLimit{Rate: 100_000, Burst: 200_000}
	return LimitConfig{ByType: limits}
}

func startMuxResponder(ctx context.Context, server *MuxLink) {
	go func() {
		for {
			stream, err := server.Accept(ctx)
			if err != nil {
				return
			}
			go func(stream *MuxStream) {
				if err := server.AuthorizeRemote(ctx, stream.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
					return
				}
				if err := stream.WriteData(ctx, []byte{1}); err == nil {
					_ = stream.CloseWrite(ctx)
				}
			}(stream)
		}
	}()
}

func runMuxTransfer(tb testing.TB, payload []byte, reverse bool, clientTLS, serverTLS *tls.Config, clientSigner, serverSigner crypto.Signer, clientID, serverID NodeID, peerVerifier PeerVerifier) error {
	// Mux throughput is intentionally measured by the benchmark output. Keep
	// the correctness deadline generous enough for slower diagnostic hosts so a
	// low throughput result is reported instead of becoming a false protocol
	// failure at a fixed 30-second boundary.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, server := benchmarkMuxPairForDirection(tb, ctx, reverse, clientTLS, serverTLS, clientSigner, serverSigner, clientID, serverID, peerVerifier)
	defer client.Close(ErrProtocol)
	defer server.Close(ErrProtocol)
	openSide, acceptSide := client, server
	if reverse {
		openSide, acceptSide = server, client
	}
	acceptDone := make(chan error, 1)
	go func() {
		for {
			stream, err := acceptSide.Accept(ctx)
			if err != nil {
				acceptDone <- err
				return
			}
			if err := acceptSide.AuthorizeRemote(ctx, stream.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
				acceptDone <- err
				return
			}
			go func(stream *MuxStream) {
				var pending uint32
				for {
					frame, err := stream.Receive(ctx)
					if err != nil {
						acceptDone <- err
						return
					}
					switch frame.Type {
					case FrameData:
						pending += uint32(len(frame.Payload))
						if pending >= 48*1024 {
							if err := stream.Consume(ctx, pending); err != nil {
								acceptDone <- err
								return
							}
							pending = 0
						}
					case FrameFIN:
						if err := stream.Consume(ctx, pending); err != nil {
							acceptDone <- err
							return
						}
						acceptDone <- stream.CloseWrite(ctx)
						return
					default:
						acceptDone <- ErrProtocol
						return
					}
				}
			}(stream)
		}
	}()
	request := OpenRequest{RuleID: [16]byte{7}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{192, 0, 2, 1}}
	stream, err := openSide.Open(ctx, request)
	if err != nil {
		return err
	}
	frame, err := stream.Receive(ctx)
	if err != nil {
		return err
	}
	if frame.Type != FrameOpenOK {
		return errors.New("vlink: expected OPEN_OK for throughput stream")
	}
	if err := stream.WriteData(ctx, payload); err != nil {
		return err
	}
	if err := stream.CloseWrite(ctx); err != nil {
		return err
	}
	frame, err = stream.Receive(ctx)
	if err != nil {
		return err
	}
	if frame.Type != FrameFIN {
		return errors.New("vlink: expected FIN after throughput response")
	}
	return <-acceptDone
}

func measureMuxFirstByte(ctx context.Context, client *MuxLink) (time.Duration, error) {
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{192, 0, 2, 1}}
	start := time.Now()
	stream, err := client.Open(ctx, request)
	if err != nil {
		return 0, err
	}
	frame, err := stream.Receive(ctx)
	if err != nil {
		return 0, err
	}
	if frame.Type != FrameOpenOK {
		return 0, fmt.Errorf("vlink: expected OPEN_OK before first byte, got type=%d stream=%d", frame.Type, frame.StreamID)
	}
	frame, err = stream.Receive(ctx)
	if err != nil {
		return 0, err
	}
	if frame.Type != FrameData || len(frame.Payload) != 1 {
		return 0, errors.New("vlink: expected one-byte target response")
	}
	if err = stream.CloseWrite(ctx); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func runTracerTransfer(tb testing.TB, payload []byte, reverse bool, clientTLS, serverTLS *tls.Config, clientSigner, serverSigner crypto.Signer, clientID, serverID NodeID, peerVerifier PeerVerifier) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	appClient, appTracer := tcpPair(tb)
	target, targetPeer := tcpPair(tb)
	clientRaw, serverRaw := tcpPair(tb)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeTCP(ctx, tls.Server(serverRaw, serverTLS.Clone()), HelloConfig{NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: peerVerifier}, func(context.Context, NodeID, OpenRequest) (net.Conn, uint16) { return target, 0 })
	}()
	clientDone := make(chan error, 1)
	go func() {
		clientDone <- TraceTCP(ctx, tls.Client(clientRaw, clientTLS.Clone()), appTracer, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{1}, Generation: 1, Signer: clientSigner, VerifyPeer: peerVerifier}, OpenRequest{RuleID: [16]byte{3}, RuleRevision: 1, PathGeneration: 1, Network: 1, ClientAddress: []byte{192, 0, 2, 1}})
	}()
	sinkDone := make(chan error, 1)
	go func() {
		if reverse {
			if _, err := writeAllAndClose(targetPeer, payload); err != nil {
				sinkDone <- err
				return
			}
			_, err := io.Copy(io.Discard, targetPeer)
			sinkDone <- err
			return
		}
		if _, err := io.Copy(io.Discard, targetPeer); err != nil {
			sinkDone <- err
			return
		}
		_ = targetPeer.CloseWrite()
		sinkDone <- nil
	}()
	if reverse {
		_ = appClient.CloseWrite()
		if _, err := io.Copy(io.Discard, appClient); err != nil {
			return err
		}
	} else {
		if _, err := writeAllAndClose(appClient, payload); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, appClient); err != nil {
			return err
		}
	}
	if err := <-sinkDone; err != nil {
		return err
	}
	_ = targetPeer.Close()
	_ = appClient.Close()
	if err := <-clientDone; err != nil {
		return err
	}
	return <-serverDone
}

func runTLSBaseline(tb testing.TB, payload []byte, clientConfig, serverConfig *tls.Config) error {
	clientRaw, serverRaw := tcpPair(tb)
	client := tls.Client(clientRaw, clientConfig.Clone())
	server := tls.Server(serverRaw, serverConfig.Clone())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handshake := make(chan error, 2)
	go func() { handshake <- client.HandshakeContext(ctx) }()
	go func() { handshake <- server.HandshakeContext(ctx) }()
	if err := <-handshake; err != nil {
		return err
	}
	if err := <-handshake; err != nil {
		return err
	}
	readDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, server); readDone <- err }()
	if _, err := client.Write(payload); err != nil {
		return err
	}
	_ = client.Close()
	readErr := <-readDone
	_ = server.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	return nil
}

func writeAllAndClose(conn *net.TCPConn, payload []byte) (int64, error) {
	var written int64
	for len(payload) > 0 {
		n, err := conn.Write(payload)
		written += int64(n)
		payload = payload[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, conn.CloseWrite()
}
