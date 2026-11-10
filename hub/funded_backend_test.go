package hub

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestFundedHeartbeatIndependentBoundsAndFixedDeadline(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	clock := store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	c := &BackendClient{HubID: uuid.NewString(), BackendID: uuid.NewString(), Region: "test", Store: s, Clock: func() store.IssuerTime { return clock }}
	session := uuid.NewString()
	hb := &controlv1.BackendHeartbeat{BackendId: c.BackendID, HubId: c.HubID, SessionId: session, RegionId: c.Region, ObservedAt: now.Add(time.Second).Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	if e = c.persistFundedHeartbeat(ctx, session, hb); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("future sender timestamp accepted without lower-bound proof", e)
	}
	cancel()
	if _, e = s.AgentState(context.Background(), "backend_heartbeat"); e == nil {
		t.Fatal("unproven future heartbeat persisted")
	}
	hb.ObservedAt = now.Format(time.RFC3339Nano)
	clock.Trusted = false
	if e = c.persistFundedHeartbeat(context.Background(), session, hb); !errors.Is(e, store.ErrAuthorityTimeUncertain) {
		t.Fatal("untrusted clock", e)
	}
	clock.Trusted = true
	clock.Latest = now.Add(11 * time.Second)
	if e = c.persistFundedHeartbeat(context.Background(), session, hb); !errors.Is(e, store.ErrAuthorityTimeUncertain) {
		t.Fatal("overwide bounds", e)
	}
	clock.Latest = now
	for i := 0; i < 2; i++ {
		if e = c.persistFundedHeartbeat(context.Background(), session, hb); e != nil {
			t.Fatal("exact heartbeat replay", e)
		}
	}
	hb.AutonomyDeadline = now.Add(2 * time.Hour).Format(time.RFC3339Nano)
	if e = c.persistFundedHeartbeat(context.Background(), session, hb); !errors.Is(e, store.ErrHeartbeatStale) {
		t.Fatal("same heartbeat extended deadline", e)
	}
	clock.Earliest, clock.Latest = now.Add(31*time.Second), now.Add(31*time.Second)
	hb.ObservedAt = clock.Latest.Format(time.RFC3339Nano)
	if e = c.persistFundedHeartbeat(context.Background(), session, hb); !errors.Is(e, store.ErrAuthorityHandoffRequired) {
		t.Fatal("reconnect reopened offline authority", e)
	}
	state, e := s.CurrentAutonomyStatus(context.Background(), clock.Latest, true)
	if e != nil || state.Mode != store.AutonomyOffline || !state.HardDeadline.Equal(now.Add(time.Hour)) {
		t.Fatal("deadline changed after refused recovery", e, state)
	}
}
