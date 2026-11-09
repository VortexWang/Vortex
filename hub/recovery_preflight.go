package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"math"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

var ErrRecoveryEvidenceRetained = errors.New("Hub S/R evidence retained; independent admission still required")

func (c *BackendClient) applyRecoveryPreflightCommand(ctx context.Context, session string, wire *controlv1.HubRecoveryCommand) (SameStoreRecoveryCommand, []byte, error) {
	var empty SameStoreRecoveryCommand
	if wire == nil || wire.BackendId != c.BackendID || wire.HubId != c.HubID || wire.BootId != c.BootID || wire.SessionId != session || wire.RegionId != c.Region || len(wire.ProtoReflect().GetUnknown()) != 0 {
		return empty, nil, ErrSameStoreRecovery
	}
	hash := sha256.Sum256(wire.Payload)
	p, e := DecodeSameStoreRecovery(wire.Payload)
	if e != nil || !bytes.Equal(hash[:], wire.PayloadSha256) || p.BackendID != c.BackendID || p.HubID != c.HubID || p.ToBootID != c.BootID {
		return empty, nil, ErrSameStoreRecovery
	}
	// First try immutable local FULL replay without a clock. First installation
	// instead needs the authenticated command's original time window.
	result, e := c.Store.HandoffHub(ctx, p.Handoff, store.IssuerTime{})
	if errors.Is(e, store.ErrAuthorityTimeUncertain) {
		if c.Clock == nil {
			return empty, nil, store.ErrAuthorityTimeUncertain
		}
		for {
			now := c.Clock()
			if !validFundedClock(now) {
				return empty, nil, store.ErrAuthorityTimeUncertain
			}
			if !now.Latest.Before(p.ExpiresAt) {
				return empty, nil, store.ErrAuthorityExpired
			}
			if !now.Earliest.Before(p.IssuedAt) {
				result, e = c.Store.HandoffHub(ctx, p.Handoff, now)
				break
			}
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return empty, nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	if e != nil {
		return empty, nil, e
	}
	raw, e := json.Marshal(SameStoreRecoveryManifest{Request: p.Handoff, Result: result})
	if e != nil {
		return empty, nil, e
	}
	if _, e = DecodeSameStoreManifest(raw, p); e != nil {
		return empty, nil, e
	}
	return p, raw, nil
}

// runRecoveryPreflight never confirms a local ledger or opens online mode.
// True merely asks Run to attempt a separately gated ordinary funded stream.
func (c *BackendClient) runRecoveryPreflight(ctx context.Context, stream grpc.BidiStreamingClient[controlv1.HubMessage, controlv1.BackendMessage]) (bool, error) {
	session := uuid.NewString()
	lastBackend := uint64(0)
	send := func(m *controlv1.HubMessage) error {
		n, e := c.Store.NextControlSequence(ctx)
		if e != nil {
			return e
		}
		m.HubId = c.HubID
		m.BootId = c.BootID
		m.Sequence = n
		return stream.Send(m)
	}
	receive := func() (*controlv1.BackendMessage, error) {
		m, e := stream.Recv()
		if e != nil {
			return nil, e
		}
		if m.Sequence == 0 || m.Sequence > math.MaxInt64 || m.Sequence <= lastBackend || !validStatusUUID(m.MessageId) || len(m.ProtoReflect().GetUnknown()) != 0 {
			return nil, ErrSameStoreRecovery
		}
		lastBackend = m.Sequence
		return m, nil
	}
	if e := send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: []string{"hub-sr-preflight-v1"}, SessionId: session, RegionId: c.Region}}}); e != nil {
		return false, e
	}
	m, e := receive()
	if e != nil {
		return false, e
	}
	if decision := m.GetError(); decision != nil {
		if len(decision.ProtoReflect().GetUnknown()) != 0 || decision.Code != "funded_session_available" || decision.Retryable {
			return false, ErrSameStoreRecovery
		}
		return true, nil
	}
	command, raw, e := c.applyRecoveryPreflightCommand(ctx, session, m.GetRecoveryCommand())
	if e != nil {
		return false, e
	}
	manifest, e := DecodeSameStoreManifest(raw, command)
	if e != nil {
		return false, e
	}
	limit := manifest.Result.SealedSequence + 1
	retain := func(record, kind string, n int64, digest []byte) error {
		m, e := receive()
		if e != nil {
			return e
		}
		r := m.GetRecoveryRetention()
		if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || r.BackendId != c.BackendID || r.HubId != c.HubID || r.BootId != c.BootID || r.SessionId != session || r.RegionId != c.Region || r.RecoveryId != command.ID || r.RecordId != record || r.Kind != kind || r.LedgerSequence != uint64(n) || !bytes.Equal(r.PayloadSha256, digest) {
			return ErrSameStoreRecovery
		}
		return nil // deliberately not Store.ConfirmLedger
	}
	cursor := int64(0)
	for cursor < limit {
		page, e := c.Store.LedgerPrefix(ctx, cursor, limit, 32)
		if e != nil {
			return false, e
		}
		if len(page) == 0 {
			return false, ErrSameStoreRecovery
		}
		for _, r := range page {
			if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_RecoveryRecord{RecoveryRecord: &controlv1.HubRecoveryRecord{RecoveryId: command.ID, Record: &controlv1.LedgerRecord{RecordId: r.ID, Kind: r.Kind, LedgerSequence: uint64(r.Sequence), Payload: r.Payload, PayloadSha256: r.Digest}}}}); e != nil {
				return false, e
			}
			if e = retain(r.ID, r.Kind, r.Sequence, r.Digest); e != nil {
				return false, e
			}
			cursor = r.Sequence
		}
	}
	hash := sha256.Sum256(raw)
	if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_RecoveryManifest{RecoveryManifest: &controlv1.HubRecoveryManifest{RecoveryId: command.ID, Payload: raw, PayloadSha256: hash[:]}}}); e != nil {
		return false, e
	}
	if e = retain(command.ID, "manifest", limit, hash[:]); e != nil {
		return false, e
	}
	return false, ErrRecoveryEvidenceRetained
}
