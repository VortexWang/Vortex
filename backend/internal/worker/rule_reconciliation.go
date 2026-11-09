package worker

import (
	"context"
	"runtime"
	"time"
)

// Drain ready work in bounded batches. Waiting after every successful task
// imposes a 10-task/s ceiling even when PG and Node are ready. Each item still
// uses the original durable lease, identity and receipt transaction in Once.
func runRuleReconciliation(ctx context.Context, once func(context.Context) (bool, error)) error {
	for {
		worked := false
		for i := 0; i < 32; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			var err error
			worked, err = once(ctx)
			if err != nil {
				return err
			}
			if !worked {
				break
			}
		}
		if worked {
			runtime.Gosched()
			continue
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
