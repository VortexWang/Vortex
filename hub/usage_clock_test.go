package hub

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

type usageClockBackend struct{ calls int }

func (b *usageClockBackend) AcceptUsage(_ context.Context, u store.HubUsage) ([]byte, error) {
	b.calls++
	return u.Digest, nil
}

func TestUsageClockSamplesAfterNewHeartbeatRead(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		t.Run(map[bool]string{true: "advanced", false: "untrusted"}[trusted], func(t *testing.T) {
			ctx := context.Background()
			s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			old := time.Now().UTC()
			newer := old.Add(time.Millisecond)
			if err := s.RecordBackendHeartbeat(ctx, old, old.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			b := store.UsageBatch{NodeID: "node", BootID: "boot", BatchID: "batch", SnapshotID: "snapshot", Payload: []byte("usage")}
			if err := s.PersistUsageLedger(ctx, b); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(b.Payload)
			backend := &usageClockBackend{}
			calls := 0
			r := UsageRelay{Store: s, Backend: backend, Clock: func() store.IssuerTime {
				calls++
				if calls == 1 {
					// A backend commit overtakes the observation before status reads.
					if err := s.RecordBackendHeartbeat(ctx, newer, old.Add(time.Hour)); err != nil {
						t.Fatal(err)
					}
					return store.IssuerTime{Earliest: old, Latest: old, Trusted: true}
				}
				return store.IssuerTime{Earliest: newer, Latest: newer, Trusted: trusted}
			}}
			err = r.ackBoundary(ctx, store.HubUsage{NodeID: b.NodeID, BootID: b.BootID, BatchID: b.BatchID, Kind: "known", Payload: b.Payload, Digest: digest[:]})
			if trusted && (err != nil || backend.calls != 1) {
				t.Fatalf("newer valid heartbeat rejected: calls=%d err=%v", backend.calls, err)
			}
			if !trusted && (err == nil || backend.calls != 0) {
				t.Fatalf("untrusted observation acknowledged: calls=%d err=%v", backend.calls, err)
			}
		})
	}
}
