package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type runtimeRateClock struct {
	boot, epoch string
	start       time.Time
	now         func() time.Time
}

// EnableMonotonicRuntimeRate is a process-local runtime selection, not authority.
// Every admission still validates trusted absolute lease bounds. Existing rate
// balances survive a new clock incarnation with no credit for downtime.
func (s *Store) EnableMonotonicRuntimeRate(boot string) error {
	if s.owner != "agent" || boot == "" {
		return ErrAuthorizationDenied
	}
	if current := s.rateClock.Load(); current != nil {
		if current.boot != boot {
			return ErrAuthorityHandoffRequired
		}
		return nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	c := &runtimeRateClock{boot: boot, epoch: hex.EncodeToString(id[:]), start: time.Now(), now: time.Now}
	if !s.rateClock.CompareAndSwap(nil, c) {
		return s.EnableMonotonicRuntimeRate(boot)
	}
	return nil
}
