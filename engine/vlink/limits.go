package vlink

import (
	"errors"
	"sync"
	"time"
)

var ErrRateLimited = errors.New("vlink: sustained control frame rate exceeded")

type FrameLimit struct {
	Rate  float64
	Burst float64
}

type LimitConfig struct {
	ByType             map[FrameType]FrameLimit
	ViolationThreshold int
}

type tokenBucket struct {
	rate, burst, tokens float64
	last                time.Time
}

type FrameLimiter struct {
	mu         sync.Mutex
	buckets    map[FrameType]*tokenBucket
	threshold  int
	violations int
	now        func() time.Time
}

func NewFrameLimiter(config LimitConfig, now func() time.Time) (*FrameLimiter, error) {
	if now == nil {
		now = time.Now
	}
	if config.ViolationThreshold == 0 {
		config.ViolationThreshold = 8
	}
	if config.ViolationThreshold < 1 {
		return nil, errors.New("vlink: violation threshold must be positive")
	}
	if config.ByType == nil {
		config.ByType = defaultFrameLimits()
	}
	buckets := make(map[FrameType]*tokenBucket, len(config.ByType))
	for frameType, limit := range config.ByType {
		if !isRateLimitedFrame(frameType) || limit.Rate <= 0 || limit.Burst < 1 || limit.Burst > 1e9 || limit.Rate > 1e9 {
			return nil, errors.New("vlink: invalid frame rate limit")
		}
		buckets[frameType] = &tokenBucket{rate: limit.Rate, burst: limit.Burst, tokens: limit.Burst, last: now()}
	}
	return &FrameLimiter{buckets: buckets, threshold: config.ViolationThreshold, now: now}, nil
}

func (l *FrameLimiter) Allow(frameType FrameType) (allowed, overLimit bool, err error) {
	if !isRateLimitedFrame(frameType) {
		return true, false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[frameType]
	if bucket == nil {
		return false, false, errors.New("vlink: control frame has no configured limiter")
	}
	now := l.now()
	if now.After(bucket.last) {
		bucket.tokens += now.Sub(bucket.last).Seconds() * bucket.rate
		if bucket.tokens > bucket.burst {
			bucket.tokens = bucket.burst
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		l.violations++
		return false, true, nil
	}
	bucket.tokens--
	return true, false, nil
}

func (l *FrameLimiter) Violation() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.violations++
	return l.violations >= l.threshold
}

func (l *FrameLimiter) Threshold() int { return l.threshold }

func isRateLimitedFrame(frameType FrameType) bool {
	switch frameType {
	case FrameOpen, FrameOpenOK, FrameOpenErr, FrameFIN, FrameRST, FramePing, FramePong, FrameWindow, FrameGoAway:
		return true
	default:
		return false
	}
}

func defaultFrameLimits() map[FrameType]FrameLimit {
	return map[FrameType]FrameLimit{
		FrameOpen: {Rate: 100, Burst: 200}, FrameOpenOK: {Rate: 100, Burst: 200}, FrameOpenErr: {Rate: 100, Burst: 200}, FrameFIN: {Rate: 1000, Burst: 2000}, FrameRST: {Rate: 1000, Burst: 2000}, FramePing: {Rate: 10, Burst: 20},
		FramePong: {Rate: 10, Burst: 20}, FrameWindow: {Rate: 1000, Burst: 2000}, FrameGoAway: {Rate: 1, Burst: 2},
	}
}
