//go:build linux

package timeauthority

import (
	"syscall"
	"time"
)

func kernelBounds() (time.Time, time.Duration, bool) {
	var tx syscall.Timex
	state, e := syscall.Adjtimex(&tx)
	// STA_UNSYNC and TIME_ERROR invalidate the kernel's synchronization claim.
	// Maxerror/Esterror are microseconds. A zero maxerror is not proof of sync.
	if e != nil || state == 5 || tx.Status&0x0040 != 0 || tx.Maxerror <= 0 || tx.Esterror < 0 {
		return time.Time{}, 0, false
	}
	bound := max(tx.Maxerror, tx.Esterror)
	if bound > 1000000 {
		return time.Time{}, 0, false
	}
	return time.Now().UTC(), time.Duration(bound) * time.Microsecond, true
}
