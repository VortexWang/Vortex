package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// runRecoveryAdmission uses a separate capability and typed response. A prior
// forensic retention receipt cannot reach the local admission transaction.
func (c *BackendClient) runRecoveryAdmission(ctx context.Context, stream grpc.BidiStreamingClient[controlv1.HubMessage, controlv1.BackendMessage]) error {
	session := uuid.NewString()
	sequence, e := c.Store.NextControlSequence(ctx)
	if e != nil {
		return e
	}
	if e = stream.Send(&controlv1.HubMessage{HubId: c.HubID, BootId: c.BootID, Sequence: sequence, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: []string{"hub-sr-admission-v1"}, SessionId: session, RegionId: c.Region}}}); e != nil {
		return e
	}
	m, e := stream.Recv()
	if e != nil {
		return e
	}
	if m.Sequence != 1 || !validStatusUUID(m.MessageId) || len(m.ProtoReflect().GetUnknown()) != 0 {
		return ErrSameStoreRecovery
	}
	if decision := m.GetError(); decision != nil {
		if decision.Code != "funded_session_available" || decision.Retryable || len(decision.ProtoReflect().GetUnknown()) != 0 {
			return ErrSameStoreRecovery
		}
		return nil // ordinary funded handshake still gates this advisory response
	}
	return c.applyRecoveryAdmission(ctx, session, m.GetRecoveryAdmission())
}

func (c *BackendClient) applyRecoveryAdmission(ctx context.Context, session string, wire *controlv1.HubRecoveryAdmission) error {
	if wire == nil || wire.BackendId != c.BackendID || wire.HubId != c.HubID || wire.BootId != c.BootID || wire.SessionId != session || wire.RegionId != c.Region || len(wire.ProtoReflect().GetUnknown()) != 0 {
		return ErrSameStoreRecovery
	}
	hash := sha256.Sum256(wire.Payload)
	p, e := store.DecodeHubRecoveryAdmission(wire.Payload)
	if e != nil || !bytes.Equal(hash[:], wire.PayloadSha256) || p.BackendID != c.BackendID || p.HubID != c.HubID || p.BootID != c.BootID {
		return ErrSameStoreRecovery
	}
	// FULL replay is historical and clock-free. A first admission must wait for
	// its authenticated timestamp to lie inside trusted conservative local time.
	_, e = c.Store.ApplyHubRecoveryAdmission(ctx, c.BackendID, c.HubID, c.BootID, wire.Payload, store.IssuerTime{})
	if !errors.Is(e, store.ErrAuthorityTimeUncertain) {
		return e
	}
	if c.Clock == nil {
		return store.ErrAuthorityTimeUncertain
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		now := c.Clock()
		if !validFundedClock(now) {
			return store.ErrAuthorityTimeUncertain
		}
		if !now.Latest.Before(p.ObservedAt.Add(5*time.Second)) || !now.Latest.Before(p.AutonomyDeadline) {
			return store.ErrAuthorityExpired
		}
		if !now.Earliest.Before(p.ObservedAt) {
			_, e = c.Store.ApplyHubRecoveryAdmission(ctx, c.BackendID, c.HubID, c.BootID, wire.Payload, now)
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return store.ErrAuthorityTimeUncertain
		case <-time.After(25 * time.Millisecond):
		}
	}
}
