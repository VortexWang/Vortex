package store

import (
	"context"
	"errors"
	"testing"
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
