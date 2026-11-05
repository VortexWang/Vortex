package agent

import (
	"errors"
	"math"
	"sync"
	"time"
)

// DemandPolicyConfig contains experiment parameters, not approved defaults.
// Amounts are raw bytes; MaxTarget bounds a request, not delegated authority.
type DemandPolicyConfig struct {
	Minimum, MaxTarget          uint64
	TargetSeconds, RenewPercent uint64
}

type consumptionBucket struct {
	second int64
	bytes  uint64
}

// DemandPolicy belongs to one immutable demand scope. Only successfully
// committed consumption is observed, never reserved or unknown upper bounds.
// Its fixed sixty buckets estimate a one-minute rate without unbounded history.
// Restart loses estimation only; durable pending request identities remain in
// Store and take precedence over a newly computed amount.
type DemandPolicy struct {
	mu               sync.Mutex
	config           DemandPolicyConfig
	buckets          [60]consumptionBucket
	last             int64
	started, faulted bool
}

func NewDemandPolicy(c DemandPolicyConfig) (*DemandPolicy, error) {
	if c.Minimum == 0 || c.Minimum > c.MaxTarget || c.MaxTarget > math.MaxInt64 || c.TargetSeconds == 0 || c.TargetSeconds > 60 || c.RenewPercent == 0 || c.RenewPercent >= 100 {
		return nil, errors.New("agent: invalid demand candidate parameters")
	}
	return &DemandPolicy{config: c}, nil
}

func (p *DemandPolicy) advance(at time.Time) (int64, error) {
	second := at.Unix()
	if p.faulted || at.IsZero() || second < 0 || p.started && second < p.last {
		p.faulted = true
		return 0, errors.New("agent: demand observation clock moved backwards or is invalid")
	}
	p.last, p.started = second, true
	return second, nil
}

func (p *DemandPolicy) ObserveCommitted(at time.Time, used uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	second, err := p.advance(at)
	if err != nil {
		return err
	}
	b := &p.buckets[second%60]
	if b.second != second {
		*b = consumptionBucket{second: second}
	}
	// Saturation cannot produce a wrapped small request. Authority remains
	// capped independently by the durable issuer's actual source balance.
	if used > math.MaxInt64-b.bytes {
		b.bytes = math.MaxInt64
	} else {
		b.bytes += used
	}
	return nil
}

type DemandEstimate struct{ MinuteBytes, Target, RequestBytes uint64 }

// Estimate uses the current second and preceding 59 second buckets. Boundary
// precision is one second. It does not count partial startup as a full-rate
// minute; the configured cold-start floor covers that interval. Available must
// come from the same scope's valid durable grant balances, excluding held and
// unknown bytes. The estimate cannot authorize spending those balances.
func (p *DemandPolicy) Estimate(at time.Time, available uint64) (DemandEstimate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	second, err := p.advance(at)
	if err != nil {
		return DemandEstimate{}, err
	}
	var total uint64
	for _, b := range p.buckets {
		if b.second <= second && b.second > second-60 {
			if b.bytes > math.MaxInt64-total {
				total = math.MaxInt64
				break
			}
			total += b.bytes
		}
	}
	return p.estimateTotal(total, available), nil
}

func (p *DemandPolicy) estimateTotal(total, available uint64) DemandEstimate {
	// ceil(total * seconds / 60), arranged to avoid multiplication overflow.
	horizon := p.config.TargetSeconds
	target := total/60*horizon + (total%60*horizon+59)/60
	if target < p.config.Minimum {
		target = p.config.Minimum
	}
	if target > p.config.MaxTarget {
		target = p.config.MaxTarget
	}
	threshold := target/100*p.config.RenewPercent + target%100*p.config.RenewPercent/100
	result := DemandEstimate{MinuteBytes: total, Target: target}
	if available <= threshold {
		result.RequestBytes = target - available
	}
	return result
}
