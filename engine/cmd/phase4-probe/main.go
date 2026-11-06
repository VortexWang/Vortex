// phase4-probe sends forged OPENs through the actual TLS/HELLO runtime boundary.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

func main() {
	path := flag.String("config", "", "source fixture config")
	out := flag.String("output", "", "JSON result")
	certs := flag.String("cert-dir", "", "optional host certificate directory")
	bypassPeer := flag.String("bypass-peer", "", "attempt direct unauthorized middle or destination hop")
	staleOnly := flag.Bool("stale-only", false, "assert the supplied old generation is rejected")
	bypassHop := flag.Uint("bypass-hop", 0, "forged direct hop index")
	flag.Parse()
	cfg, err := config.LoadFile(*path)
	check(err)
	if *certs != "" {
		cfg.Certificate = filepath.Join(*certs, "source.pem")
		cfg.PrivateKey = filepath.Join(*certs, "source-key.pem")
		cfg.TrustBundle = filepath.Join(*certs, "ca.pem")
	}
	if *bypassPeer != "" {
		cfg.Peer = *bypassPeer
		// Deliberate adversarial probe: bypass the configured next-hop pin and
		// test the receiving runtime policy using the real authenticated identity.
		cfg.NextHopNodeID = ""
	}
	s, err := link.New(cfg)
	check(err)
	defer s.Close()
	digest := sha256.Sum256([]byte(cfg.RuleID))
	base := vlink.OpenRequest{RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, HopIndex: cfg.HopIndex, Network: 1, ClientAddress: []byte{127, 0, 0, 1}}
	copy(base.RuleID[:], digest[:16])
	local, err := vlink.ParseNodeID(cfg.PeerNodeID)
	check(err)
	tests := []struct {
		name   string
		code   vlink.OpenErrorCode
		mutate func(*vlink.OpenRequest)
	}{
		{"unknown-rule", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleID[0] ^= 255 }},
		{"stale-revision", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleRevision-- }},
		{"stale-generation", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.PathGeneration-- }},
		{"future-revision", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleRevision++ }},
		{"future-generation", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.PathGeneration++ }},
		{"skipped-hop", vlink.OpenNoRoute, func(r *vlink.OpenRequest) { r.HopIndex++ }},
		{"visited-local-loop", vlink.OpenNoRoute, func(r *vlink.OpenRequest) { r.Visited = []vlink.NodeID{local} }},
		{"duplicate-visited", vlink.OpenProtocolError, func(r *vlink.OpenRequest) { r.Visited = []vlink.NodeID{{42}, {42}} }},
	}
	results := make(map[string]any)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = s.Dial(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
		if *staleOnly {
			stream, err := mux.Open(ctx, base)
			if err != nil {
				return err
			}
			frame, err := stream.Receive(ctx)
			if err != nil {
				return err
			}
			defer frame.Release()
			if frame.Type != vlink.FrameOpenErr || len(frame.Payload) != 18 || vlink.OpenErrorCode(binary.BigEndian.Uint16(frame.Payload)) != vlink.OpenStaleRevision {
				return fmt.Errorf("old generation OPEN was not rejected as stale")
			}
			results["old-generation"] = map[string]any{"status": "pass", "code": vlink.OpenStaleRevision}
			return nil
		}
		if *bypassPeer != "" {
			request := base
			request.HopIndex = uint16(*bypassHop)
			stream, err := mux.Open(ctx, request)
			if err != nil {
				return err
			}
			frame, err := stream.Receive(ctx)
			if err != nil {
				return err
			}
			if frame.Type != vlink.FrameOpenErr || len(frame.Payload) != 18 || vlink.OpenErrorCode(binary.BigEndian.Uint16(frame.Payload)) != vlink.OpenUnauthorized {
				return fmt.Errorf("direct bypass was not rejected as unauthorized")
			}
			results["direct-bypass"] = map[string]any{"status": "pass", "code": vlink.OpenUnauthorized}
			return nil
		}
		for _, network := range []uint8{1, 2} {
			for _, test := range tests {
				request := base
				request.Network = network
				name := fmt.Sprintf("%s/%s", map[uint8]string{1: "tcp", 2: "udp"}[network], test.name)
				test.mutate(&request)
				stream, err := mux.Open(ctx, request)
				if err != nil {
					return err
				}
				frame, err := stream.Receive(ctx)
				if err != nil {
					return err
				}
				if frame.Type != vlink.FrameOpenErr || len(frame.Payload) != 18 {
					return fmt.Errorf("%s accepted or missing rejection", name)
				}
				code := vlink.OpenErrorCode(binary.BigEndian.Uint16(frame.Payload))
				if code != test.code || string(frame.Payload[2:]) == string(make([]byte, 16)) {
					return fmt.Errorf("%s wrong rejection code/correlation", name)
				}
				results[name] = map[string]any{"status": "pass", "code": code, "correlation_id": fmt.Sprintf("%x", frame.Payload[2:])}
				frame.Release()
			}
		}
		// Closing a live link must terminate an authorized UDP stream, without
		// retaining the previous session for transparent replay.
		request := base
		request.Network = 2
		stream, err := mux.Open(ctx, request)
		if err != nil {
			return err
		}
		if err = stream.WaitOpen(ctx); err != nil {
			return err
		}
		mux.Close(context.Canceled)
		waitCtx, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		_, err = stream.Receive(waitCtx)
		if err == nil || err == context.DeadlineExceeded {
			return fmt.Errorf("UDP link loss did not terminate stream: %v", err)
		}
		results["udp-link-loss"] = map[string]any{"status": "pass"}
		return nil
	})
	check(err)
	data, err := json.MarshalIndent(map[string]any{"status": "pass", "cases": results, "scope": "runtime TLS/HELLO, static local policy, UDP session link loss; not exhaustive fault qualification"}, "", "  ")
	check(err)
	check(os.WriteFile(*out, append(data, '\n'), 0644))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
