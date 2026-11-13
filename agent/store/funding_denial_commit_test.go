package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestFullyDeniedFundingGroupDoesNotCommitRolledBackPages(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil {
		t.Fatal(err)
	}
	observer, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	conn, err := observer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var versionBefore, versionAfter int
	if err := conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&versionBefore); err != nil {
		t.Fatal(err)
	}
	for _, denial := range []error{ErrAuthorizationDenied, ErrAuthorityTimeUncertain, &RateLimited{}} {
		results := s.commitFundingAdmissionRequests([]*fundingAdmissionRequest{{ctx: ctx, size: 1, run: func(tx *runtimeTransaction) error {
			bounds := &fundingBlockRange{minimum: 100, preferred: 1000, admit: true}
			if _, err := s.reserveRuntimeUsageDirect(ctx, runtimeReservation("denied", 1000), a.Use, true, bounds, tx); err != nil {
				return err
			}
			return denial
		}}})
		if len(results) != 1 || !errors.Is(results[0], denial) {
			t.Fatalf("denial changed: %v", results)
		}
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&versionAfter); err != nil {
		t.Fatal(err)
	}
	if versionBefore != versionAfter {
		t.Fatalf("fully rolled back denials committed pages: data_version %d -> %d", versionBefore, versionAfter)
	}
	after, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || before != after {
		t.Fatalf("denial changed funds: %+v %+v %v", before, after, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denial retained rate: %d %v", count, err)
	}
}
