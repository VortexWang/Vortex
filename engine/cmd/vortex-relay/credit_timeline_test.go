package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	accounting "vortex/engine/runtime/accounting"
)

func TestCreditTimelineBoundedFinalAndConcurrentStop(t *testing.T) {
	var used atomic.Int64
	stop := startCreditTimeline(func() accounting.CreditState { return accounting.CreditState{Budget: 100, Used: used.Load()} }, time.Millisecond, 1)
	// Cap is occupied by the initial sample. The final state must still appear.
	time.Sleep(10 * time.Millisecond)
	used.Store(42)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := stop()
			if len(result.Samples) != 2 || result.Samples[0].State.Used != 0 || result.Samples[1].State.Used != 42 || result.Samples[1].ElapsedNanos < result.Samples[0].ElapsedNanos {
				t.Errorf("invalid timeline: %+v", result)
			}
		}()
	}
	wg.Wait()
	before := stop()
	used.Store(99)
	if after := stop(); after.Samples[1].State.Used != 42 || after.Dropped != before.Dropped {
		t.Fatal("sampler remained live after stop")
	}
}
