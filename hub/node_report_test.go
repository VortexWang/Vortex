package hub

import (
	"context"
	"testing"
	"time"

	controlv1 "vortex/contracts/protobuf/v1"
)

func TestCurrentNodeReportFencesOldConnectionAndCopiesSnapshot(t *testing.T) {
	s := new(TestServer)
	ctx := context.Background()
	node := "550e8400-e29b-41d4-a716-446655440000"
	r := &controlv1.NodeReport{ReportId: node, BuildVersion: "dev", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), DrainState: "running"}
	if err := s.beginReportSession(node, "boot1", "session1"); err != nil {
		t.Fatal(err)
	}
	if err := s.acceptReport(ctx, node, "boot1", "session1", r); err != nil {
		t.Fatal(err)
	}
	r.BuildVersion = "changed"
	if got := s.CurrentNodeReport(node); got == nil || got.Report.BuildVersion != "dev" {
		t.Fatalf("aliased input: %+v", got)
	}
	if err := s.beginReportSession(node, "boot2", "session2"); err != nil {
		t.Fatal(err)
	}
	if err := s.acceptReport(ctx, node, "boot1", "session1", r); err == nil {
		t.Fatal("old connection overwrote snapshot")
	}
	s.endReportSession(node, "session1")
	if err := s.acceptReport(ctx, node, "boot2", "session2", r); err != nil {
		t.Fatal(err)
	}
	copy := s.CurrentNodeReport(node)
	copy.Report.BuildVersion = "forged"
	if got := s.CurrentNodeReport(node); got == nil || got.Report.BuildVersion != "changed" {
		t.Fatalf("aliased output: %+v", got)
	}
	s.endReportSession(node, "session2")
	if s.CurrentNodeReport(node) != nil {
		t.Fatal("disconnected telemetry remained current")
	}
}
