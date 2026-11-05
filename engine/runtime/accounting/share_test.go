package accounting

import (
	"context"
	"testing"
	"time"

	core "vortex/engine/accounting"
	"vortex/engine/share"
)

func TestShareGateAcquiresBeforeOpenAndReleasesExactlyOnce(t *testing.T) {
	manager := share.NewManager()
	key := share.Key{}
	key.Node[0] = 1
	key.Boot[0] = 1
	r, err := manager.Prepare(key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Commit(key, r.Generation); err != nil {
		t.Fatal(err)
	}
	inner, _ := NewCreditGate(CreditConfig{SnapshotID: "s", Budget: 1, InitialBlock: 1, MaxBlock: 1, GrantTarget: 1, RenewPercent: 30})
	gate := ShareGate{Manager: manager, Key: key, Inner: inner}
	session, err := gate.Open(context.Background(), core.Flow{Key: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "over-capacity"}); err == nil {
		t.Fatal("share capacity was not enforced by runtime Gate")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reserve(context.Background(), core.LeftToRight, 1); err == nil {
		t.Fatal("closed share session admitted more reservations")
	}
	state, _ := manager.Get(key)
	if state.InUse != 0 {
		t.Fatalf("Gate close leaked share lease: %+v", state)
	}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "after-close"}); err != nil {
		t.Fatal(err)
	}
}

func TestShareGateRollsBackShareIfInnerOpenFails(t *testing.T) {
	manager := share.NewManager()
	key := share.Key{}
	key.Node[0] = 1
	key.Boot[0] = 1
	r, _ := manager.Prepare(key, 1, 1)
	_, _ = manager.Commit(key, r.Generation)
	gate := ShareGate{Manager: manager, Key: key, Inner: rejectGate{}}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "rejected"}); err == nil {
		t.Fatal("inner gate unexpectedly admitted")
	}
	state, _ := manager.Get(key)
	if state.InUse != 0 {
		t.Fatalf("failed Open leaked share lease: %+v", state)
	}
}

func TestShareGateRejectsExpiredLeaseBeforeAcquiring(t *testing.T) {
	manager := share.NewManager()
	key := share.Key{}
	key.Node[0] = 4
	key.Boot[0] = 4
	revision, err := manager.Prepare(key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Commit(key, revision.Generation); err != nil {
		t.Fatal(err)
	}
	inner, _ := NewCreditGate(CreditConfig{SnapshotID: "s", Budget: 1, InitialBlock: 1, MaxBlock: 1, GrantTarget: 1, RenewPercent: 30})
	gate := ShareGate{Manager: manager, Key: key, Inner: inner, LeaseUntil: time.Now().Add(-time.Second)}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "expired"}); err == nil {
		t.Fatal("expired share lease admitted a flow")
	}
	state, _ := manager.Get(key)
	if state.InUse != 0 {
		t.Fatalf("expired admission consumed share capacity: %+v", state)
	}
}

type rejectGate struct{}

func (rejectGate) Open(context.Context, core.Flow) (core.Session, error) { return nil, core.ErrDenied }

func TestShareSessionStopsAtLeaseDeadlineAndRetainsUnresolvedUsage(t *testing.T) {
	manager := share.NewManager()
	key := share.Key{}
	key.Node[0], key.Boot[0] = 5, 5
	revision, err := manager.Prepare(key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Commit(key, revision.Generation); err != nil {
		t.Fatal(err)
	}
	inner := creditFixture(t, 256)
	now := time.Unix(1800000000, 0)
	deadline := now.Add(time.Second)
	gate := ShareGate{Manager: manager, Key: key, Inner: inner, LeaseUntil: deadline, now: func() time.Time { return now }}
	session, err := gate.Open(context.Background(), core.Flow{Key: "lease-bound"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(context.Background(), core.LeftToRight, 32)
	if err != nil {
		t.Fatal(err)
	}
	now = deadline
	if _, err := session.Reserve(context.Background(), core.LeftToRight, 1); err == nil {
		t.Fatal("existing flow wrote at expired lease boundary")
	}
	state, _ := manager.Get(key)
	if state.InUse != 1 {
		t.Fatal("expiration released unresolved share ownership")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if inner.State().Held != 32 {
		t.Fatal("close released unresolved usage")
	}
	if err := batch.MarkUnknown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if inner.State().Unknown != 32 {
		t.Fatal("unknown liability not frozen")
	}
	conserved(t, inner)
}

func TestShareSessionReturnsReservationIfLeaseExpiresDuringInnerWait(t *testing.T) {
	manager := share.NewManager()
	key := share.Key{}
	key.Node[0], key.Boot[0] = 6, 6
	revision, err := manager.Prepare(key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Commit(key, revision.Generation); err != nil {
		t.Fatal(err)
	}
	inner := creditFixture(t, 256)
	now := time.Unix(1800000000, 0)
	deadline := now.Add(time.Second)
	reads := 0
	gate := ShareGate{Manager: manager, Key: key, Inner: inner, LeaseUntil: deadline, now: func() time.Time {
		reads++
		if reads >= 4 {
			return deadline
		}
		return now
	}}
	session, err := gate.Open(context.Background(), core.Flow{Key: "inner-wait"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reserve(context.Background(), core.LeftToRight, 32); err == nil {
		t.Fatal("reservation escaped after lease expired during inner wait")
	}
	session.Close()
	if state := inner.State(); state.Used != 0 || state.Unknown != 0 || state.Held != 0 || state.Sessions != 0 {
		t.Fatalf("expired reservation not returned: %+v", state)
	}
	conserved(t, inner)
}
