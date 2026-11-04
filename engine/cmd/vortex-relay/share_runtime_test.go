package main

import (
	"context"
	"testing"
	"time"

	core "vortex/engine/accounting"
	"vortex/engine/runtime/accounting"
	"vortex/engine/runtime/config"
)

func TestConfigureShareGateFencesAndLimitsRuntimeAdmission(t *testing.T) {
	cfg := config.Config{
		NodeID: "00000000-0000-0000-0000-000000000001",
		Share: &config.ShareConfig{
			BootID:        "00000000-0000-0000-0000-000000000002",
			TransactionID: "00000000-0000-0000-0000-000000000003",
			Revision:      1,
			Rate:          1,
			Connections:   1,
			LeaseUntil:    time.Now().Add(time.Minute),
		},
	}
	inner, err := accounting.NewCreditGate(accounting.CreditConfig{SnapshotID: "s", Budget: 4, InitialBlock: 1, MaxBlock: 1, GrantTarget: 1, RenewPercent: 30})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := configureShareGate(cfg, inner)
	if err != nil {
		t.Fatal(err)
	}
	first, err := gate.Open(context.Background(), core.Flow{Key: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "over-capacity"}); err == nil {
		t.Fatal("runtime share capacity was not enforced")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Open(context.Background(), core.Flow{Key: "after-drain"}); err != nil {
		t.Fatalf("share lease was not released after close: %v", err)
	}
}
