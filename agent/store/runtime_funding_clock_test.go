package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBlockFundingClockRefreshAfterWriterQueue(t *testing.T) {
	for _, path := range []string{"first", "existing", "batch"} {
		for _, mode := range []string{"active", "expired", "untrusted", "revoked", "canceled"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				s, scope, _ := runtimeStore(t)
				ctx := context.Background()
				session, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
				if err != nil {
					t.Fatal(err)
				}
				block := runtimeReservation("block", 256)
				if path != "first" {
					if _, err := s.ReserveRuntimeFundingBlock(ctx, block, session.Use); err != nil {
						t.Fatal(err)
					}
				}
				before, err := s.AuditAuthorizationSource(ctx, "node", "funding")
				if err != nil {
					t.Fatal(err)
				}
				if mode == "revoked" {
					if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "revoke", Payload: []byte("revoke"), IssuerID: "hub-one", SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, nil, []string{"grant"}); err != nil {
						t.Fatal(err)
					}
				}
				stale := session.Use
				fresh := scope.Earliest
				if mode == "active" {
					stale.Earliest, stale.Latest = fresh.Add(-2*time.Second), fresh.Add(-2*time.Second)
				}
				if mode == "expired" {
					fresh = fresh.Add(10 * time.Minute)
				}
				called := make(chan struct{}, 1)
				clock := func() (time.Time, time.Time, bool) {
					if s.db.Stats().InUse != 1 {
						t.Error("clock sampled before transaction owned connection")
					}
					called <- struct{}{}
					return fresh, fresh, mode != "untrusted"
				}
				release, err := s.runtimeQueue.acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					switch path {
					case "first":
						_, err := s.ReserveAndAdmitRuntimeFundingBlock(requestCtx, block, stale, 20, nil, clock)
						done <- err
					case "existing":
						done <- s.AdmitRuntimeFundingBlockWrite(requestCtx, block, stale, 20, clock)
					default:
						done <- s.AdmitRuntimeFundingBlockBatch(requestCtx, []RuntimeBlockAdmission{{Block: block, Scope: stale, Bytes: 10}, {Block: block, Scope: stale, Bytes: 10}}, clock)
					}
				}()
				waitRuntimeQueue(t, &s.runtimeQueue, 1)
				if len(called) != 0 {
					t.Fatal("clock sampled while writer was still queued")
				}
				if mode == "canceled" {
					cancel()
				}
				release()
				select {
				case err = <-done:
				case <-time.After(6 * time.Second):
					t.Fatal("queued funding did not finish")
				}
				if mode == "canceled" {
					if !errors.Is(err, context.Canceled) || len(called) != 0 {
						t.Fatalf("canceled operation ran: %v clock=%d", err, len(called))
					}
				} else if len(called) != 1 {
					t.Fatalf("clock calls=%d", len(called))
				}
				after, e := s.AuditAuthorizationSource(ctx, "node", "funding")
				if e != nil {
					t.Fatal(e)
				}
				var buckets int
				if e := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); e != nil {
					t.Fatal(e)
				}
				if mode == "active" {
					if err != nil || buckets != 1 {
						t.Fatalf("current valid authority denied: %v buckets=%d", err, buckets)
					}
					if path == "first" {
						before.Reserved += 256
						before.Remaining -= 256
					}
					if before != after {
						t.Fatalf("funding mismatch %+v %+v", before, after)
					}
					var tokens uint64
					if e := s.db.QueryRow(`SELECT tokens FROM runtime_rate_buckets`).Scan(&tokens); e != nil || tokens != 65536-20 {
						t.Fatalf("rate debit=%d %v", tokens, e)
					}
				} else if err == nil || before != after || buckets != 0 {
					t.Fatalf("invalid refreshed authority mutated state: %v before=%+v after=%+v buckets=%d", err, before, after, buckets)
				}
			})
		}
	}
}
