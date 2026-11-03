package vlink

import (
	"testing"
	"time"
)

func TestLimitBurstAndSustainedThreshold(t *testing.T) {
	now := time.Unix(0, 0)
	limiter, err := NewFrameLimiter(LimitConfig{ByType: map[FrameType]FrameLimit{FramePing: {Rate: 1, Burst: 2}}, ViolationThreshold: 3}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		allowed, over, err := limiter.Allow(FramePing)
		if err != nil || !allowed || over {
			t.Fatalf("burst allowed=%v over=%v err=%v", allowed, over, err)
		}
	}
	for i := 0; i < 2; i++ {
		allowed, over, err := limiter.Allow(FramePing)
		if err != nil || allowed || !over {
			t.Fatalf("excess %d allowed=%v over=%v err=%v", i, allowed, over, err)
		}
	}
	now = now.Add(time.Second)
	allowed, _, err := limiter.Allow(FramePing)
	if err != nil || !allowed {
		t.Fatalf("refill allowed=%v err=%v", allowed, err)
	}
	if !limiter.Violation() || !limiter.Violation() {
		t.Fatal("violation threshold did not accumulate")
	}
}

func TestLimitRejectsUnconfiguredAndInvalidLimits(t *testing.T) {
	if _, err := NewFrameLimiter(LimitConfig{ByType: map[FrameType]FrameLimit{FramePing: {Rate: 0, Burst: 1}}}, nil); err == nil {
		t.Fatal("accepted zero rate")
	}
	limiter, err := NewFrameLimiter(LimitConfig{ByType: map[FrameType]FrameLimit{FramePing: {Rate: 1, Burst: 1}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := limiter.Allow(FrameOpen); err == nil {
		t.Fatal("accepted control frame without configured bucket")
	}
}
