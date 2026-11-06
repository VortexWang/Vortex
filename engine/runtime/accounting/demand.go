package accounting

import "time"

// demandWindow is a bounded test-adapter estimator, protected by CreditGate.mu.
// It counts committed raw bytes only. The monotonic clock starts on first use;
// one-second buckets cover the current second and its 59 predecessors.
type demandWindow struct {
	start   time.Time
	now     func() time.Time
	buckets [60]struct{ second, bytes int64 }
}

func (w *demandWindow) second() int64 {
	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	if w.start.IsZero() {
		w.start = now
	}
	return max(0, int64(now.Sub(w.start)/time.Second))
}

func (w *demandWindow) observe(used int64) {
	second := w.second()
	b := &w.buckets[second%60]
	if b.second != second {
		b.second, b.bytes = second, 0
	}
	// Total committed bytes cannot exceed the validated 2^50 finite budget.
	b.bytes += used
}

func (g *CreditGate) target() int64 {
	if g.cfg.TargetSeconds == 0 {
		return g.cfg.GrantTarget
	}
	second := g.demand.second()
	var total int64
	for _, b := range g.demand.buckets {
		if b.second <= second && b.second > second-60 {
			total += b.bytes
		}
	}
	// ceil(total * horizon / 60), without overflowing multiplication.
	target := total/60*g.cfg.TargetSeconds + (total%60*g.cfg.TargetSeconds+59)/60
	return min(g.cfg.GrantTarget, max(8<<20, target))
}
