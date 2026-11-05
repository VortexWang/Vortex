package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func hooks(failedHealth bool, rolledBack *bool) Hooks {
	check := func(context.Context) error { return nil }
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	artifact := []byte("signed upgrade artifact")
	return Hooks{Artifact: artifact, Signature: ed25519.Sign(priv, artifact), PublicKey: pub, VerifyBinary: check, VerifyDisk: check, VerifyConfig: check, Drain: func(context.Context, time.Duration) error { return nil }, Snapshot: func(context.Context) ([]byte, error) { return []byte("compatible-state"), nil }, RestoreSnapshot: func(context.Context, []byte) error { return nil }, Activate: func(context.Context) error { return nil }, Health: func(context.Context) error {
		if failedHealth {
			return errors.New("bad health")
		}
		return nil
	}, Rollback: func(context.Context) error { *rolledBack = true; return nil }}
}

func TestFailedHealthRollsBack(t *testing.T) {
	var rb bool
	if err := Run(context.Background(), hooks(true, &rb), time.Second); err == nil || !rb {
		t.Fatalf("err=%v rollback=%v", err, rb)
	}
}
func TestIrreversibleMigrationBlocksRollback(t *testing.T) {
	var rb bool
	h := hooks(true, &rb)
	h.IrreversibleMigration = true
	if err := Run(context.Background(), h, time.Second); !errors.Is(err, ErrIncompatible) || rb {
		t.Fatalf("err=%v rollback=%v", err, rb)
	}
}

func TestDrainReceivesBoundedContext(t *testing.T) {
	var rb bool
	h := hooks(false, &rb)
	h.Drain = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	start := time.Now()
	if err := Run(context.Background(), h, 20*time.Millisecond); err == nil {
		t.Fatal("drain timeout ignored")
	}
	if time.Since(start) > time.Second {
		t.Fatal("drain exceeded bounded timeout")
	}
}

func TestHealthFailureRestoresCompatibilitySnapshotBeforeRollback(t *testing.T) {
	var rb bool
	var restored bool
	h := hooks(true, &rb)
	h.RestoreSnapshot = func(_ context.Context, snapshot []byte) error {
		if string(snapshot) != "compatible-state" {
			t.Fatalf("snapshot=%q", snapshot)
		}
		restored = true
		return nil
	}
	h.Rollback = func(context.Context) error {
		if !restored {
			t.Fatal("rollback preceded state restore")
		}
		rb = true
		return nil
	}
	if err := Run(context.Background(), h, time.Second); err == nil || !restored || !rb {
		t.Fatalf("err=%v restored=%v rollback=%v", err, restored, rb)
	}
}
