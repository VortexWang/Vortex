package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"vortex/contracts"
)

func TestUsageEnvelopePayloadOriginAndJournalCommitAtomically(t *testing.T) {
	for _, kind := range []string{"known", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "hub.db")
			s, err := Open(path, "hub")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`CREATE TRIGGER fail_usage_origin BEFORE INSERT ON usage_origin_envelopes BEGIN SELECT RAISE(ABORT,'origin disk error'); END`); err != nil {
				t.Fatal(err)
			}
			known := UsageBatch{NodeID: "node", BootID: "origin-boot", BatchID: "one", SnapshotID: "snapshot", Payload: []byte("known-payload")}
			unknown := unknownPayload(t, "one")
			persist := func(reportBoot string, sequence uint64) (string, uint64, error) {
				if kind == "known" {
					return s.PersistUsageLedgerWithOrigin(ctx, known, reportBoot, sequence)
				}
				return s.PersistUnknownUsageWithOrigin(ctx, "node", reportBoot, sequence, unknown)
			}
			if boot, seq, err := persist("report-one", 7); err == nil || boot != "" || seq != 0 {
				t.Fatal("failed origin exposed a commit", boot, seq, err)
			}
			for _, table := range []string{"usage_batches", "unknown_usage_batches", "usage_origin_envelopes", "hub_ledger", "usage_backend_receipts"} {
				var count int
				if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial %s survived origin failure: %d %v", table, count, err)
				}
			}
			if _, err := s.db.Exec(`DROP TRIGGER fail_usage_origin`); err != nil {
				t.Fatal(err)
			}
			if boot, seq, err := persist("report-one", 7); err != nil || boot != "report-one" || seq != 7 {
				t.Fatal("envelope commit", boot, seq, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if boot, seq, err := persist("report-two", 19); err != nil || boot != "report-one" || seq != 7 {
				t.Fatal("replay replaced durable provenance", boot, seq, err)
			}
			pending, err := s.PendingBackendUsage(ctx)
			if err != nil || len(pending) != 1 || pending[0].ReportBootID != "report-one" || pending[0].Sequence != 7 || pending[0].Kind != kind {
				t.Fatalf("unconfirmed envelope disappeared: %+v %v", pending, err)
			}
			var confirmed int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_backend_receipts`).Scan(&confirmed); err != nil || confirmed != 0 {
				t.Fatal("envelope commit fabricated backend receipt", confirmed, err)
			}
			var ledger int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM hub_ledger`).Scan(&ledger); err != nil || ledger != 1 {
				t.Fatal("replay duplicated journal", ledger, err)
			}
			if kind == "unknown" {
				var transport, liability string
				if err := s.db.QueryRow(`SELECT transport_state,liability_state FROM unknown_usage_batches`).Scan(&transport, &liability); err != nil || transport != "received" || liability != "unresolved" {
					t.Fatal("unknown liability changed", transport, liability, err)
				}
			}
		})
	}
}

func TestUsageEnvelopeRejectsConflictsAndSequenceReuse(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := UsageBatch{NodeID: "node", BootID: "boot", BatchID: "one", SnapshotID: "snapshot", Payload: []byte("first")}
	if _, _, err := s.PersistUsageLedgerWithOrigin(ctx, b, "report", 1); err != nil {
		t.Fatal(err)
	}
	changed := b
	changed.Payload = []byte("conflicting")
	if _, _, err := s.PersistUsageLedgerWithOrigin(ctx, changed, "report", 2); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("conflicting payload accepted", err)
	}
	var disputes int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_disputes`).Scan(&disputes); err != nil || disputes != 1 {
		t.Fatal("lost conflict evidence", disputes, err)
	}
	changed = b
	changed.BatchID = "two"
	if _, _, err := s.PersistUsageLedgerWithOrigin(ctx, changed, "report", 1); err == nil {
		t.Fatal("accepted sequence reuse")
	}
	var leaked int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches WHERE batch_id='two'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("sequence conflict left orphan payload", leaked, err)
	}
	unknown := unknownPayload(t, "unknown")
	if _, _, err := s.PersistUnknownUsageWithOrigin(ctx, "node", "report", 3, unknown); err != nil {
		t.Fatal(err)
	}
	// Change valid typed content while retaining the same batch identity.
	decoded, err := contracts.DecodeUnknownUsage(unknown)
	if err != nil {
		t.Fatal(err)
	}
	decoded.UnknownUpperBound++
	conflictingUnknown, err := contracts.EncodeUnknownUsage(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PersistUnknownUsageWithOrigin(ctx, "node", "report", 4, conflictingUnknown); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("conflicting unknown accepted", err)
	}
	var unknownConflicts int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM unknown_usage_conflicts`).Scan(&unknownConflicts); err != nil || unknownConflicts != 1 {
		t.Fatal("lost unknown conflict evidence", unknownConflicts, err)
	}
	knownConflict := UsageBatch{NodeID: "node", BootID: decoded.OriginBootId, BatchID: decoded.BatchId, SnapshotID: decoded.BillingSnapshotId, Payload: []byte("ordinary overwrite")}
	if _, _, err := s.PersistUsageLedgerWithOrigin(ctx, knownConflict, "report", 4); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("ordinary usage overwrote unknown liability", err)
	}
}
