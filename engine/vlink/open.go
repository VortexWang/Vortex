package vlink

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
)

type OpenErrorCode uint16

const (
	OpenNoRoute           OpenErrorCode = 1
	OpenUnauthorized      OpenErrorCode = 2
	OpenQuotaExhausted    OpenErrorCode = 3
	OpenRateLimited       OpenErrorCode = 4
	OpenConnectionLimited OpenErrorCode = 5
	OpenTargetUnreachable OpenErrorCode = 6
	OpenStaleRevision     OpenErrorCode = 7
	OpenProtocolError     OpenErrorCode = 8
)

type OpenAuditEvent struct {
	CorrelationID [16]byte
	PeerID        NodeID
	RuleID        [16]byte
	Code          OpenErrorCode
}
type RuleSnapshot struct {
	RuleID               [16]byte
	Revision             uint64
	PathGeneration       uint64
	HopIndex             uint16
	ExecutionPoint       bool
	Active               bool
	AllowedPeers         map[NodeID]bool
	AllowedNetworks      map[uint8]bool
	NextHops             []NodeID
	RuntimeAuthorized    bool
	SegmentAllowed       bool
	RateShareValid       bool
	ConnectionShareValid bool
	BillingAuthorized    bool
	BillingSnapshotID    string
	BillingCoefficient   uint64
	SelfHosted           bool
	Target               LocalTarget
}

type RuleSnapshotLookup func(context.Context, [16]byte) (RuleSnapshot, bool)
type OpenAuditor func(OpenAuditEvent)
type OpenPolicy struct {
	LocalNode                 NodeID
	Lookup                    RuleSnapshotLookup
	Audit                     OpenAuditor
	SessionBillingSnapshotID  string
	SessionBillingCoefficient uint64
}

type OpenDecision struct {
	NextHop  NodeID
	Snapshot RuleSnapshot
	Target   LocalTarget
}
type OpenRejection struct {
	Code          OpenErrorCode
	CorrelationID [16]byte
}

func (e *OpenRejection) Error() string {
	return fmt.Sprintf("vlink: OPEN rejected (class %d, correlation %x)", e.Code, e.CorrelationID)
}

func (p OpenPolicy) Authorize(ctx context.Context, peer NodeID, request OpenRequest) (OpenDecision, error) {
	reject := func(code OpenErrorCode) (OpenDecision, error) {
		var correlation [16]byte
		_, _ = rand.Read(correlation[:])
		if p.Audit != nil {
			p.Audit(OpenAuditEvent{CorrelationID: correlation, PeerID: peer, RuleID: request.RuleID, Code: code})
		}
		return OpenDecision{}, &OpenRejection{Code: code, CorrelationID: correlation}
	}
	if p.Lookup == nil || p.LocalNode == (NodeID{}) || peer == (NodeID{}) {
		return reject(OpenUnauthorized)
	}
	if request.Network != 1 && request.Network != 2 {
		return reject(OpenProtocolError)
	}
	if !validOriginalClient(request.ClientAddress) || len(request.Visited) > 16 {
		return reject(OpenProtocolError)
	}
	seenVisited := make(map[NodeID]struct{}, len(request.Visited))
	for _, visited := range request.Visited {
		if visited == (NodeID{}) {
			return reject(OpenProtocolError)
		}
		if _, duplicate := seenVisited[visited]; duplicate {
			return reject(OpenProtocolError)
		}
		seenVisited[visited] = struct{}{}
	}
	snapshot, ok := p.Lookup(ctx, request.RuleID)
	if !ok || !snapshot.Active {
		return reject(OpenNoRoute)
	}
	if snapshot.RuleID != request.RuleID || request.RuleRevision != snapshot.Revision || request.PathGeneration != snapshot.PathGeneration {
		return reject(OpenStaleRevision)
	}
	if request.HopIndex != snapshot.HopIndex {
		return reject(OpenNoRoute)
	}
	if !snapshot.AllowedNetworks[request.Network] {
		return reject(OpenUnauthorized)
	}
	if !snapshot.AllowedPeers[peer] || !snapshot.RuntimeAuthorized || !snapshot.SegmentAllowed {
		return reject(OpenUnauthorized)
	}
	for _, visited := range request.Visited {
		if visited == p.LocalNode {
			return reject(OpenNoRoute)
		}
	}
	if snapshot.ExecutionPoint && !snapshot.SelfHosted {
		if !snapshot.RateShareValid {
			return reject(OpenRateLimited)
		}
		if !snapshot.ConnectionShareValid {
			return reject(OpenConnectionLimited)
		}
		if !snapshot.BillingAuthorized || snapshot.BillingSnapshotID == "" || snapshot.BillingSnapshotID != p.SessionBillingSnapshotID || snapshot.BillingCoefficient == 0 || snapshot.BillingCoefficient != p.SessionBillingCoefficient {
			return reject(OpenQuotaExhausted)
		}
	}
	for _, candidate := range snapshot.NextHops {
		if candidate == (NodeID{}) {
			continue
		}
		visited := false
		for _, id := range request.Visited {
			if id == candidate {
				visited = true
				break
			}
		}
		if !visited {
			return OpenDecision{NextHop: candidate, Snapshot: snapshot, Target: snapshot.Target}, nil
		}
	}
	return reject(OpenNoRoute)
}

func validOriginalClient(raw []byte) bool {
	var address netip.Addr
	switch len(raw) {
	case 4:
		var bytes [4]byte
		copy(bytes[:], raw)
		address = netip.AddrFrom4(bytes)
	case 16:
		var bytes [16]byte
		copy(bytes[:], raw)
		address = netip.AddrFrom16(bytes)
	default:
		return false
	}
	return address.IsValid() && !address.IsUnspecified() && !address.IsMulticast()
}

func OpenErrorPayload(rejection *OpenRejection) ([]byte, error) {
	if rejection == nil || rejection.Code < OpenNoRoute || rejection.Code > OpenProtocolError || rejection.CorrelationID == ([16]byte{}) {
		return nil, errors.New("vlink: valid OPEN rejection required")
	}
	payload := make([]byte, 18)
	payload[0] = byte(rejection.Code >> 8)
	payload[1] = byte(rejection.Code)
	copy(payload[2:], rejection.CorrelationID[:])
	return payload, nil
}

func newOpenRejection(code OpenErrorCode) *OpenRejection {
	var correlation [16]byte
	_, _ = rand.Read(correlation[:])
	return &OpenRejection{Code: code, CorrelationID: correlation}
}
