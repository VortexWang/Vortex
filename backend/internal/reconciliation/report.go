// Package reconciliation defines the bounded backend qualification result.
// It does not equate a deterministic Node adapter with real Engine execution.
package reconciliation

import (
	"fmt"
	"time"
)

type Report struct {
	Commit          string    `json:"commit"`
	Scope           string    `json:"scope"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	ElapsedNS       int64     `json:"elapsed_ns"`
	ElapsedMS       float64   `json:"elapsed_ms"`
	Rules           int       `json:"rules"`
	Resolved        int       `json:"resolved"`
	Members         int       `json:"members"`
	Observed        int       `json:"observed"`
	InventorySHA256 string    `json:"inventory_sha256"`
	Status          string    `json:"status"`
}

func (r Report) Validate() error {
	if r.Status != "passed" || r.Rules != 1000 || r.Resolved != 1000 || r.Members != 2000 || r.Observed != 2000 {
		return fmt.Errorf("incomplete 1000-rule, 2000-member reconciliation")
	}
	if r.ElapsedNS < 0 || r.ElapsedNS > int64(60*time.Second) || r.ElapsedMS != float64(r.ElapsedNS)/float64(time.Millisecond) || r.EndedAt.Before(r.StartedAt) {
		return fmt.Errorf("reconciliation exceeded exact 60-second budget or has invalid timing")
	}
	if len(r.InventorySHA256) != 64 {
		return fmt.Errorf("missing exact inventory digest")
	}
	return nil
}
