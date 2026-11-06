package main

import (
	"sync"
	"time"
	accounting "vortex/engine/runtime/accounting"
)

type creditSample struct {
	ElapsedNanos int64                  `json:"elapsed_nanoseconds"`
	State        accounting.CreditState `json:"state"`
}

type creditTimeline struct {
	IntervalNanos int64          `json:"interval_nanoseconds"`
	Dropped       uint64         `json:"dropped_periodic_samples"`
	Samples       []creditSample `json:"samples"`
}

// A bounded, opt-in diagnostic. Periodic observations are not exact peaks or a
// time-weighted average. A final sample is retained even after the cap is hit.
// Stop is idempotent and joins the sampler before returning the immutable result.
func startCreditTimeline(state func() accounting.CreditState, interval time.Duration, limit int) func() creditTimeline {
	start := time.Now()
	result := creditTimeline{IntervalNanos: int64(interval), Samples: make([]creditSample, 0, limit+1)}
	sample := func() creditSample {
		return creditSample{ElapsedNanos: time.Since(start).Nanoseconds(), State: state()}
	}
	result.Samples = append(result.Samples, sample())
	stop, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				result.Samples = append(result.Samples, sample())
				return
			case <-ticker.C:
				if len(result.Samples) < limit {
					result.Samples = append(result.Samples, sample())
				} else {
					result.Dropped++
				}
			}
		}
	}()
	return func() creditTimeline { once.Do(func() { close(stop) }); <-done; return result }
}
