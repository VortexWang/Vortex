package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Independent sessions share a commit, but a rejected member must not retain
// funding or rate debits. Uncertain failures must roll back the whole commit.
func TestCrossSessionFundingAdmissionOutcomes(t *testing.T) {
	for _, outcome := range []string{"success", "denied", "storage-error", "cancel-before-commit"} {
		t.Run(outcome, func(t *testing.T) {
			s, scope, path := runtimeStore(t)
			ctx := context.Background()
			if _, err := s.db.Exec(`UPDATE authorization_records SET connections='2' WHERE kind='share_lease'`); err != nil {
				t.Fatal(err)
			}
			memberCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			failure := errors.New("injected storage failure")
			var requests []*fundingAdmissionRequest
			for i, name := range []string{"first", "second"} {
				a, err := s.OpenRuntimeSession(ctx, scope, name, "flow-"+name)
				if err != nil {
					t.Fatal(err)
				}
				block := runtimeReservation(name, 1000)
				block.FlowKey = "flow-" + name
				requestCtx := ctx
				if i == 1 {
					requestCtx = memberCtx
				}
				requests = append(requests, &fundingAdmissionRequest{ctx: requestCtx, size: 1, run: func(tx *runtimeTransaction) error {
					bounds := &fundingBlockRange{minimum: 100, preferred: 1000, admit: true}
					if _, err := s.reserveRuntimeUsageDirect(requestCtx, block, a.Use, true, bounds, tx); err != nil {
						return err
					}
					if i == 1 {
						switch outcome {
						case "denied":
							return ErrAuthorizationDenied
						case "storage-error":
							return failure
						case "cancel-before-commit":
							cancel()
						}
					}
					return nil
				}})
			}
			errs := s.commitFundingAdmissionRequests(requests)
			wantBlocks := 2
			switch outcome {
			case "success":
				if errs[0] != nil || errs[1] != nil {
					t.Fatalf("valid admissions failed: %v", errs)
				}
			case "denied":
				wantBlocks = 1
				if errs[0] != nil || !errors.Is(errs[1], ErrAuthorizationDenied) {
					t.Fatalf("denial escaped its savepoint: %v", errs)
				}
			default:
				wantBlocks = 0
				for _, err := range errs {
					if err == nil {
						t.Fatal("failed group issued write permission")
					}
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			balance, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Reserved != uint64(wantBlocks)*1000 || balance.Remaining != (1<<20)-uint64(wantBlocks)*1000 || balance.Committed != 0 || balance.Unknown != 0 {
				t.Fatalf("durable conservation: %+v %v", balance, err)
			}
			var blocks, buckets int
			if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM runtime_funding_blocks`).Scan(&blocks); err != nil || blocks != wantBlocks {
				t.Fatalf("partial funding records: %d %v", blocks, err)
			}
			if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); err != nil {
				t.Fatal(err)
			}
			if wantBlocks == 0 {
				if buckets != 0 {
					t.Fatal("rolled back admission retained rate debit")
				}
			} else {
				var tokens uint64
				if err := reopened.db.QueryRow(`SELECT tokens FROM runtime_rate_buckets WHERE node_id='node'`).Scan(&tokens); err != nil || tokens != 65536-uint64(wantBlocks)*100 {
					t.Fatalf("rate debit escaped member outcome: %d %v", tokens, err)
				}
			}
		})
	}
}

// Arrivals during an occupied writer slot must join the next bounded commit,
// rather than waiting behind a prematurely detached singleton transaction.
func TestFundingQueueCollectsArrivalsDuringWriterWait(t *testing.T) {
	s, _, _ := runtimeStore(t)
	ctx := context.Background()
	release, err := s.runtimeQueue.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 16)
	transactions := make(chan *runtimeTransaction, 16)
	submit := func(i int) {
		go func() {
			done <- s.enqueueFundingOperation(ctx, 1, func(tx *runtimeTransaction) error {
				transactions <- tx
				_, err := tx.ExecContext(ctx, `INSERT INTO durable_events(event_id,owner,kind,payload,created_at) VALUES(?,?,?,?,?)`, fmt.Sprint(i), "agent", "queue-test", []byte("fact"), "now")
				return err
			})
		}()
	}
	submit(0)
	waitRuntimeQueue(t, &s.runtimeQueue, 1)
	for i := 1; i < 16; i++ {
		submit(i)
	}
	deadline := time.Now().Add(time.Second)
	for {
		s.fundingAdmissions.mu.Lock()
		count := s.fundingAdmissions.count
		s.fundingAdmissions.mu.Unlock()
		if count == 16 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callers did not queue")
		}
		time.Sleep(time.Millisecond)
	}
	release()
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[*runtimeTransaction]bool{}
	for i := 0; i < 16; i++ {
		seen[<-transactions] = true
	}
	if len(seen) != 1 {
		t.Fatalf("writer wait split one bounded batch across %d commits", len(seen))
	}
	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM durable_events WHERE kind='queue-test'`).Scan(&rows); err != nil || rows != 16 {
		t.Fatalf("durable facts: %d %v", rows, err)
	}
}

// A reconnect may cancel sequence/ACK persistence while business admissions
// remain live. Delivery and funding must never share that cancellation domain.
func TestDeliveryCancellationDoesNotCancelFundingCommit(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	session, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 1000)
	release, err := s.runtimeQueue.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	funding := make(chan error, 1)
	go func() {
		funding <- s.enqueueFundingOperation(ctx, 1, func(tx *runtimeTransaction) error {
			_, err := s.reserveRuntimeUsageDirect(ctx, block, session.Use, true, &fundingBlockRange{minimum: 100, preferred: 1000, admit: true}, tx)
			return err
		})
	}()
	waitRuntimeQueue(t, &s.runtimeQueue, 1)
	deliveryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	delivery := make(chan error, 1)
	go func() { _, err := s.NextControlSequenceRange(deliveryCtx, 32); delivery <- err }()
	waitRuntimeQueue(t, &s.runtimeQueue, 2)
	cancel()
	if err := <-delivery; err == nil {
		t.Fatal("canceled delivery succeeded")
	}
	select {
	case err := <-funding:
		t.Fatalf("delivery cancellation affected waiting funding: %v", err)
	default:
	}
	release()
	if err := <-funding; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	balance, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Reserved != 1000 || balance.Remaining != (1<<20)-1000 || balance.Unknown != 0 {
		t.Fatalf("funding after canceled delivery: %+v %v", balance, err)
	}
	first, err := reopened.NextControlSequenceRange(ctx, 32)
	if err != nil || first != 1 {
		t.Fatalf("canceled sequence changed counter: %d %v", first, err)
	}
}
