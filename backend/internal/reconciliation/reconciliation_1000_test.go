package reconciliation

import (
	"strings"
	"testing"
	"time"
)

func TestWithin60sRejectsPartialAndOverBudget(t *testing.T) {
	at := time.Now().UTC()
	good := Report{Status: "passed", Rules: 1000, Resolved: 1000, Members: 2000, Observed: 2000, StartedAt: at, EndedAt: at.Add(time.Second), ElapsedNS: int64(time.Second), ElapsedMS: 1000, InventorySHA256: strings.Repeat("a", 64)}
	if e := good.Validate(); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*Report){
		"partial":        func(r *Report) { r.Resolved = 999 },
		"missing-member": func(r *Report) { r.Observed = 1999 },
		"over-budget-by-one-ns": func(r *Report) {
			r.ElapsedNS = int64(60*time.Second) + 1
			r.ElapsedMS = float64(r.ElapsedNS) / float64(time.Millisecond)
		},
		"rounded-duration": func(r *Report) { r.ElapsedMS = 0 },
		"failed":           func(r *Report) { r.Status = "failed" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			mutate(&bad)
			if bad.Validate() == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
