package agent

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestDemandPolicyMinuteWindowAndWaterline(t *testing.T) {
	p, err := NewDemandPolicy(DemandPolicyConfig{Minimum: 8 << 20, MaxTarget: 1 << 40, TargetSeconds: 30, RenewPercent: 30})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1800000000, 0)
	cold, err := p.Estimate(at, 0)
	if err != nil || cold.Target != 8<<20 || cold.RequestBytes != 8<<20 {
		t.Fatalf("cold start: %+v %v", cold, err)
	}
	// One full minute at 100 Mbps yields a 375 MB target. The bucket window
	// expires each observation rather than retaining a lifetime average.
	for i := 0; i < 60; i++ {
		if err := p.ObserveCommitted(at.Add(time.Duration(i)*time.Second), 12500000); err != nil {
			t.Fatal(err)
		}
	}
	end := at.Add(59 * time.Second)
	for _, available := range []uint64{112500001, 112500000, 0} {
		got, err := p.Estimate(end, available)
		if err != nil || got.Target != 375000000 || got.MinuteBytes != 750000000 {
			t.Fatalf("estimate %+v %v", got, err)
		}
		want := uint64(0)
		if available <= 112500000 {
			want = 375000000 - available
		}
		if got.RequestBytes != want {
			t.Fatalf("waterline %+v", got)
		}
	}
	expired, err := p.Estimate(at.Add(119*time.Second), 0)
	if err != nil || expired.MinuteBytes != 0 || expired.Target != 8<<20 {
		t.Fatalf("stale consumption retained %+v %v", expired, err)
	}
	if err := p.ObserveCommitted(at, 1); err == nil {
		t.Fatal("rollback accepted")
	}
	if _, err := p.Estimate(at.Add(time.Hour), 0); err == nil {
		t.Fatal("faulted estimator silently resumed")
	}
}

func TestDemandPolicySaturationAndConcurrentConsumption(t *testing.T) {
	p, err := NewDemandPolicy(DemandPolicyConfig{Minimum: 1, MaxTarget: math.MaxInt64, TargetSeconds: 60, RenewPercent: 99})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1800000000, 0)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.ObserveCommitted(at, math.MaxUint64); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := p.Estimate(at, 0)
	if err != nil || got.Target != math.MaxInt64 || got.RequestBytes != math.MaxInt64 {
		t.Fatalf("overflow %+v %v", got, err)
	}
	capped, _ := NewDemandPolicy(DemandPolicyConfig{Minimum: 8 << 20, MaxTarget: 16 << 20, TargetSeconds: 30, RenewPercent: 30})
	if err := capped.ObserveCommitted(at, math.MaxUint64); err != nil {
		t.Fatal(err)
	}
	got, err = capped.Estimate(at, math.MaxUint64)
	if err != nil || got.Target != 16<<20 || got.RequestBytes != 0 {
		t.Fatalf("cap/available overflow %+v %v", got, err)
	}
}
