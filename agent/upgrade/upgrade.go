package upgrade

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"
)

var ErrIncompatible = errors.New("upgrade: irreversible migration blocks rollback")

type Hooks struct {
	Artifact              []byte
	Signature             []byte
	PublicKey             ed25519.PublicKey
	VerifyBinary          func(context.Context) error
	VerifyDisk            func(context.Context) error
	VerifyConfig          func(context.Context) error
	Drain                 func(context.Context, time.Duration) error
	Health                func(context.Context) error
	Activate              func(context.Context) error
	Rollback              func(context.Context) error
	IrreversibleMigration bool
}

func Run(ctx context.Context, h Hooks, drainTimeout time.Duration) error {
	if len(h.Artifact) == 0 || len(h.Signature) == 0 || len(h.PublicKey) != ed25519.PublicKeySize || !ed25519.Verify(h.PublicKey, h.Artifact, h.Signature) {
		return errors.New("upgrade: artifact signature invalid")
	}
	for name, check := range map[string]func(context.Context) error{"binary": h.VerifyBinary, "disk": h.VerifyDisk, "config": h.VerifyConfig} {
		if check == nil {
			return fmt.Errorf("upgrade: %s preflight missing", name)
		}
		if err := check(ctx); err != nil {
			return fmt.Errorf("upgrade: %s preflight: %w", name, err)
		}
	}
	if h.Drain == nil || h.Health == nil || h.Activate == nil || h.Rollback == nil {
		return errors.New("upgrade: complete hooks required")
	}
	if drainTimeout <= 0 {
		return errors.New("upgrade: positive drain timeout required")
	}
	drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()
	if err := h.Drain(drainCtx, drainTimeout); err != nil {
		return fmt.Errorf("upgrade: drain: %w", err)
	}
	if err := h.Activate(ctx); err != nil {
		return fmt.Errorf("upgrade: activate: %w", err)
	}
	if err := h.Health(ctx); err != nil {
		if h.IrreversibleMigration {
			return errors.Join(ErrIncompatible, err)
		}
		if rollbackErr := h.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return fmt.Errorf("upgrade: health check: %w", err)
	}
	return nil
}
