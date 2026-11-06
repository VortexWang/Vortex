package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"strconv"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

// ForwardingProbe uses an explicitly provisioned authorized peer identity and
// existing rule. It cannot invent a rule/target/grant or bypass node accounting.
// Request/Response describe the configured target's application health exchange.
type ForwardingProbe struct {
	Peer     config.Config `json:"peer"`
	Request  []byte        `json:"request"`
	Response []byte        `json:"response"`
}

func (p ForwardingProbe) Check(ctx context.Context) error {
	if p.Peer.Role != config.RoleSource || p.Peer.QualificationOnly || len(p.Request) == 0 || len(p.Request) > 1024 || len(p.Response) == 0 || len(p.Response) > 1024 {
		return errors.New("upgrade: bounded authorized forwarding probe required")
	}
	// This is a bounded authenticated wire probe, not a business source Runtime.
	// The existing exit rule/Agent gate still authorizes and accounts every byte.
	// Do not invent a source budget merely to satisfy the business-runtime schema.
	if len(p.Peer.RuleID) == 0 || len(p.Peer.RuleID) > 128 || p.Peer.RuleRevision == 0 || p.Peer.PathGeneration == 0 || p.Peer.HopIndex > 16 {
		return errors.New("upgrade: invalid existing probe rule")
	}
	if _, err := vlink.ParseNodeID(p.Peer.NextHopNodeID); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(p.Peer.Peer)
	if err != nil || host == "" {
		return errors.New("upgrade: invalid probe peer endpoint")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("upgrade: invalid probe peer port")
	}

	supervisor, err := link.New(p.Peer.Effective())
	if err != nil {
		return err
	}
	defer supervisor.Close()
	supervisor.TLS.ServerName = p.Peer.NextHopNodeID
	return supervisor.Dial(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
		sum := sha256.Sum256([]byte(p.Peer.RuleID))
		var rule [16]byte
		copy(rule[:], sum[:16])
		stream, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: p.Peer.RuleRevision, PathGeneration: p.Peer.PathGeneration, HopIndex: p.Peer.HopIndex, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			return err
		}
		if err = stream.WaitOpen(ctx); err != nil {
			return err
		}
		if err = stream.WriteData(ctx, p.Request); err != nil {
			return err
		}
		if err = stream.CloseWrite(ctx); err != nil {
			return err
		}
		var response []byte
		for {
			frame, err := stream.Receive(ctx)
			if err != nil {
				return err
			}
			switch frame.Type {
			case vlink.FrameData:
				response = append(response, frame.Payload...)
				frame.Release()
				if len(response) > len(p.Response) {
					return errors.New("upgrade: oversized probe response")
				}
			case vlink.FrameFIN:
				frame.Release()
				if !bytes.Equal(response, p.Response) {
					return errors.New("upgrade: forwarding response mismatch")
				}
				return nil
			default:
				frame.Release()
				return errors.New("upgrade: unexpected forwarding response")
			}
		}
	})
}
