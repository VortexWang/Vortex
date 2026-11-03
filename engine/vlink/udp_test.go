package vlink

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"vortex/engine/accounting"
)

func TestUDPResourceForwardsDatagramsAndStopsOnLinkLoss(t *testing.T) {
	target, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		buffer := make([]byte, MaxPayloadSize)
		for {
			n, peer, readErr := target.ReadFromUDP(buffer)
			if readErr != nil {
				return
			}
			_, _ = target.WriteToUDP(buffer[:n], peer)
		}
	}()
	conn, err := net.DialUDP("udp", nil, target.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	config := MuxConfig{LocalClient: true, StreamWindow: 64, AggregateWindow: 64, MaxWindow: 1024}
	client, err := newMuxLink(clientConn, config)
	if err != nil {
		t.Fatal(err)
	}
	config.LocalClient = false
	server, err := newMuxLink(serverConn, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{192, 0, 2, 1}}
	clientStream, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	serverStream, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resource := &UDPResource{Conn: conn}
	if err := server.AuthorizeRemote(ctx, serverStream.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return resource, nil }); err != nil {
		t.Fatal(err)
	}
	if frame, err := clientStream.Receive(ctx); err != nil || frame.Type != FrameOpenOK {
		t.Fatalf("OPEN response=%+v err=%v", frame, err)
	}

	runDone := make(chan error, 1)
	go func() { runDone <- resource.Run(ctx, serverStream, nil) }()
	for _, payload := range []string{"same", "same"} {
		accepted, err := clientStream.WriteDgram(ctx, []byte(payload))
		if err != nil || !accepted {
			t.Fatal(err)
		}
		frame, err := clientStream.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type != FrameDgram || string(frame.Payload) != payload {
			t.Fatalf("echo frame=%+v", frame)
		}
	}

	client.Close(ErrProtocol)
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("UDP stream did not stop after link loss")
	}
	server.Close(ErrProtocol)
}

func TestUDPOverWindowDropsWithoutBlockingAndResumesAfterCredit(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	config := MuxConfig{LocalClient: true, StreamWindow: 4, AggregateWindow: 4, MaxWindow: 16}
	client, err := newMuxLink(clientConn, config)
	if err != nil {
		t.Fatal(err)
	}
	config.LocalClient = false
	server, err := newMuxLink(serverConn, config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ErrProtocol)
	defer server.Close(ErrProtocol)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{192, 0, 2, 1}}
	clientStream, err := client.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	serverStream, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AuthorizeRemote(ctx, serverStream.ID, func(context.Context, NodeID, OpenRequest) (io.Closer, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if frame, err := clientStream.Receive(ctx); err != nil || frame.Type != FrameOpenOK {
		t.Fatalf("OPEN response=%+v err=%v", frame, err)
	}
	if accepted, err := clientStream.WriteDgram(ctx, []byte("four")); err != nil || !accepted {
		t.Fatalf("initial datagram accepted=%v err=%v", accepted, err)
	}
	if accepted, err := clientStream.WriteDgram(ctx, []byte("x")); err != nil || accepted {
		t.Fatalf("over-window datagram accepted=%v err=%v", accepted, err)
	}
	if frame, err := serverStream.Receive(ctx); err != nil || string(frame.Payload) != "four" {
		t.Fatalf("first datagram=%+v err=%v", frame, err)
	}
	if err := serverStream.Consume(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if accepted, err := clientStream.WriteDgram(ctx, []byte("x")); err != nil || !accepted {
		t.Fatalf("datagram after credit accepted=%v err=%v", accepted, err)
	}
	if frame, err := serverStream.Receive(ctx); err != nil || string(frame.Payload) != "x" {
		t.Fatalf("resumed datagram=%+v err=%v", frame, err)
	}
}

func TestUDPAdmissionUsesAuthorizedLocalTargetAndStableFlowKey(t *testing.T) {
	peer := NodeID{1}
	var openedKey string
	gate := &udpAdmissionGate{open: func(flowKey string) { openedKey = flowKey }}
	policy := testOpenPolicy(peer, NodeID{2}, LocalTarget{Host: "8.8.8.8", Port: 53})
	admission := UDPAdmission{
		Policy: policy,
		Gate:   gate,
		Dial: func(context.Context, netip.AddrPort) (net.Conn, error) {
			left, right := net.Pipe()
			_ = right.Close()
			return left, nil
		},
	}
	request := OpenRequest{RuleID: [16]byte{1}, RuleRevision: 1, PathGeneration: 1, Network: 2, ClientAddress: []byte{192, 0, 2, 1}}
	resource, err := admission.Authorize(context.Background(), peer, request)
	if err != nil {
		t.Fatal(err)
	}
	defer resource.Close()
	if openedKey != peer.String()+"/01000000000000000000000000000000" {
		t.Fatalf("flow key=%q", openedKey)
	}
	if _, ok := resource.(*UDPResource); !ok {
		t.Fatalf("resource type=%T", resource)
	}
}

func testOpenPolicy(peer, next NodeID, target LocalTarget) OpenPolicy {
	ruleID := [16]byte{1}
	return OpenPolicy{
		LocalNode: NodeID{2},
		Lookup: func(context.Context, [16]byte) (RuleSnapshot, bool) {
			return RuleSnapshot{
				RuleID: ruleID, Revision: 1, PathGeneration: 1, Active: true,
				AllowedPeers: map[NodeID]bool{peer: true}, AllowedNetworks: map[uint8]bool{2: true},
				NextHops: []NodeID{next}, RuntimeAuthorized: true, SegmentAllowed: true, Target: target,
			}, true
		},
	}
}

type udpAdmissionGate struct{ open func(string) }

func (g *udpAdmissionGate) Open(_ context.Context, flow accounting.Flow) (accounting.Session, error) {
	g.open(flow.Key)
	return udpAdmissionSession{}, nil
}

type udpAdmissionSession struct{}

func (udpAdmissionSession) Reserve(context.Context, accounting.Direction, int64) (*accounting.Batch, error) {
	return nil, nil
}
func (udpAdmissionSession) Close() error { return nil }
