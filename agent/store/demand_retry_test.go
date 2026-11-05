package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDemandRefusalBackoffSurvivesRestartAndReplay(t *testing.T) {
	_, d, _, _ := issuerFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	scope := DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: d.SignerEpoch, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration}
	r, err := s.PrepareAutomaticDemand(ctx, scope, 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.DenyDemand(ctx, d.NodeID, d.BootID, d.HubID, r.ID, "insufficient_funds"); err != nil {
			t.Fatal(err)
		}
	}
	var failures int
	var denied, retry string
	if err := s.db.QueryRow(`SELECT failures,denied_at,retry_at FROM demand_retry`).Scan(&failures, &denied, &retry); err != nil || failures != 1 {
		t.Fatalf("duplicate denial extended backoff: %d %v", failures, err)
	}
	s.Close()
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareAutomaticDemand(ctx, scope, 256, time.Minute); !errors.Is(err, ErrDemandRetryDeferred) {
		t.Fatalf("restart bypassed cooldown: %v", err)
	}
	// A trusted explicit reduced request can proceed. Automatic retry of that
	// already-persisted request still coalesces, rather than inventing an ID.
	manual, err := s.PrepareDemand(ctx, scope, 128, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.PrepareAutomaticDemand(ctx, scope, 256, time.Minute)
	if err != nil || same != manual {
		t.Fatalf("pending request lost during cooldown: %+v %v", same, err)
	}
	if err := s.DenyDemand(ctx, d.NodeID, d.BootID, d.HubID, manual.ID, "expired"); err != nil {
		t.Fatal(err)
	}
	key, _ := json.Marshal(scope)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := checkDemandRetryTx(ctx, tx, key, time.Now().Add(24*time.Hour)); !errors.Is(err, ErrDemandRetryDeferred) {
		t.Fatalf("expired scope automatically retried: %v", err)
	}
}

func TestDemandRetryIsBoundedAndRejectsClockRollback(t *testing.T) {
	s, _, _, _ := issuerFixture(t)
	ctx := context.Background()
	key := []byte(`{"scope":"test"}`)
	at := time.Unix(1800000000, 0)
	for i := 0; i < 12; i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		now := at.Add(time.Duration(i) * time.Hour)
		if err := recordDemandRetryTx(ctx, tx, key, "insufficient_funds", now); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := checkDemandRetryTx(ctx, tx, key, now); !errors.Is(err, ErrDemandRetryDeferred) {
			tx.Rollback()
			t.Fatalf("immediate retry allowed: %v", err)
		}
		if err := checkDemandRetryTx(ctx, tx, key, now.Add(time.Minute)); err != nil {
			tx.Rollback()
			t.Fatalf("backoff exceeded cap: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := checkDemandRetryTx(ctx, tx, key, at); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("rollback permitted retry: %v", err)
	}
	if err := recordDemandRetryTx(ctx, tx, key, "insufficient_funds", at); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("rollback overwrote cooldown: %v", err)
	}
}
