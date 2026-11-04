package accounting

import (
	"context"
	"testing"

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

type rejectGate struct{}

func (rejectGate) Open(context.Context, core.Flow) (core.Session, error) { return nil, core.ErrDenied }
