package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestUsageOriginReplayAfterReportBootChangePreservesOriginal(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	payload := unknownPayload(t, "unknown")
	if err := s.PersistUnknownUsage(ctx, "node", payload); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsageOrigin(ctx, "node", "unknown", "report-boot-one", 7, payload); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsageOrigin(ctx, "node", "unknown", "report-boot-two", 19, payload); err != nil {
		t.Fatal("authenticated identical retry after reboot cannot reach original acknowledgement", err)
	}
	boot, sequence, err := s.UsageOrigin(ctx, "node", "unknown")
	if err != nil || boot != "report-boot-one" || sequence != 7 {
		t.Fatal("retry replaced original provenance", boot, sequence, err)
	}
	if err := s.RecordUsageOrigin(ctx, "node", "unknown", "report-boot-two", 20, append(payload, 0)); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("changed retry accepted", err)
	}
}
