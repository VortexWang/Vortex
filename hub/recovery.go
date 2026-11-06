package hub

import (
	"context"
	"fmt"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// LedgerBackend is the authenticated, durable backend boundary. Phase 5 uses
// a protocol-test-only implementation; an ACK is valid only after its commit.
type LedgerBackend interface {
	AcceptLedger(context.Context, store.LedgerRecord) ([]byte, error)
}

// Recover installs backend revocations before transferring R and replaying the
// sealed prefix and ongoing tail. Retrying the same handoff does not refill R.
func (i *Issuer) Recover(ctx context.Context, request store.HandoffRequest, backend LedgerBackend) (store.HandoffResult, error) {
	if i == nil || i.Store == nil || i.Clock == nil || backend == nil || request.HubID != i.HubID {
		return store.HandoffResult{}, fmt.Errorf("hub: recovery dependencies unavailable")
	}
	result, err := i.Store.HandoffHub(ctx, request, i.Clock())
	if err != nil {
		return result, err
	}
	if err = i.ReplayLedger(ctx, backend); err != nil {
		return result, err
	}
	return result, nil
}

// ReplayLedger is bounded so continuous new traffic cannot starve recovery.
// The next invocation resumes from persisted receipts without clearing debt.
func (i *Issuer) ReplayLedger(ctx context.Context, backend LedgerBackend) (resultErr error) {
	if i == nil || i.Store == nil || backend == nil {
		return fmt.Errorf("hub: ledger backend unavailable")
	}
	// A post-barrier tail error must not hold a confirmed finite prefix hostage.
	// Completion still checks current time, signer ownership and all prefix holes
	// transactionally, including on a lost/invalid later receipt.
	defer func() {
		if i.Clock != nil {
			if _, err := i.Store.CompleteRecovery(ctx, i.Clock()); resultErr == nil {
				resultErr = err
			}
		}
	}()
	records, err := i.Store.PendingLedger(ctx, 1000)
	if err != nil {
		return err
	}
	for _, record := range records {
		ack, err := backend.AcceptLedger(ctx, record)
		if err != nil {
			return err
		}
		if _, err = i.Store.ConfirmLedger(ctx, record, ack); err != nil {
			return err
		}
	}
	// A missing clock never establishes retention age. Authority replay still
	// works, but optional compaction waits for trusted conservative time.
	if i.Clock != nil {
		now := i.Clock()
		if now.Trusted && !now.Earliest.IsZero() && !now.Latest.Before(now.Earliest) {
			_, err = i.Store.PruneSettledUsage(ctx, now, 1000)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// RecoveryUpdate includes post-handoff revocations independently of new grants.
// Callers deliver it before requesting successor issues on the control stream.
func (i *Issuer) RecoveryUpdate(ctx context.Context, handoff, node, boot string, generation uint64) (*controlv1.AuthorizationUpdate, error) {
	if i == nil || i.Store == nil || i.Clock == nil {
		return nil, fmt.Errorf("hub: recovery issuer unavailable")
	}
	result, err := i.Store.RecoveryAuthorization(ctx, handoff, node, boot, generation, i.Clock())
	if err != nil {
		return nil, err
	}
	return &controlv1.AuthorizationUpdate{Generation: result.Generation, Payload: result.Payload}, nil
}
