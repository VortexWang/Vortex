//go:build !linux

package timeauthority

import "time"

func kernelBounds() (time.Time, time.Duration, bool) { return time.Time{}, 0, false }
