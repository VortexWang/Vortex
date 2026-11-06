package contracts

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestNodeReportRejectsUnboundedAndUnknownTelemetry(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	r := &controlv1.NodeReport{ReportId: id, BuildVersion: "dev", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), DrainState: "running", Probes: []*controlv1.ProbeObservation{{TargetId: id, State: "unknown", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}
	if err := ValidateNodeReport(r); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*controlv1.NodeReport){
		func(r *controlv1.NodeReport) { r.Probes = append(r.Probes, r.Probes[0]) },
		func(r *controlv1.NodeReport) { r.Probes[0].Failures = 4 },
		func(r *controlv1.NodeReport) { r.Probes[0].State = "up" },
		func(r *controlv1.NodeReport) { r.Probes[0].ObservedAt = "" },
		func(r *controlv1.NodeReport) { r.Probes[0].ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
		func(r *controlv1.NodeReport) { r.ActiveSessions = 1000001 },
		func(r *controlv1.NodeReport) { r.DrainState = "assumed" },
	} {
		bad := proto.Clone(r).(*controlv1.NodeReport)
		mutate(bad)
		if err := ValidateNodeReport(bad); err == nil {
			t.Fatal("bad telemetry accepted")
		}
	}
}
