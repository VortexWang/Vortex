package contracts

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// ValidateNodeReport implements the approved bounded telemetry projection.
// A valid report is never a trusted heartbeat or a financial operation.
func ValidateNodeReport(r *controlv1.NodeReport) error {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || proto.Size(r) > 1<<20 || !canonicalUsageUUID.MatchString(r.ReportId) || r.BuildVersion == "" || len(r.BuildVersion) > 128 || !utf8.ValidString(r.BuildVersion) || r.AppliedConfigVersion > math.MaxInt64 || r.ActiveSessions > 1000000 || len(r.Probes) > 1024 {
		return fmt.Errorf("node report: invalid bounded envelope")
	}
	if r.DrainState != "running" && r.DrainState != "draining" && r.DrainState != "stopped" {
		return fmt.Errorf("node report: invalid drain state")
	}
	validTime := func(s string) bool {
		if len(s) > 30 || !strings.HasSuffix(s, "Z") {
			return false
		}
		_, err := time.Parse(time.RFC3339Nano, s)
		return err == nil
	}
	if !validTime(r.ObservedAt) {
		return fmt.Errorf("node report: invalid timestamp")
	}
	seen := make(map[string]bool, len(r.Probes))
	for _, p := range r.Probes {
		if p == nil || len(p.ProtoReflect().GetUnknown()) != 0 || !canonicalUsageUUID.MatchString(p.TargetId) || seen[p.TargetId] || !validTime(p.ObservedAt) || p.Failures > 3 || p.Successes > 2 || p.LatencyUs > 1000000 || (p.State != "unknown" && p.State != "healthy" && p.State != "unhealthy") {
			return fmt.Errorf("node report: invalid or duplicate probe")
		}
		seen[p.TargetId] = true
	}
	return nil
}
