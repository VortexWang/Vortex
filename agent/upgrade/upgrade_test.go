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
	return Hooks{Artifact: artifact, Signature: ed25519.Sign(priv, artifact), PublicKey: pub, VerifyBinary: check, VerifyDisk: check, VerifyConfig: check, Drain: func(context.Context, time.Duration) error { return nil }, Activate: func(context.Context) error { return nil }, Health: func(context.Context) error {
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
