package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

var fundedCapabilities = []string{"status-v1", "backend-allocation-v1", "backend-ledger-v1"}

func (c *BackendClient) runFunded(ctx context.Context, stream grpc.BidiStreamingClient[controlv1.HubMessage, controlv1.BackendMessage]) error {
	session := uuid.NewString()
	send := func(body *controlv1.HubMessage) error {
		sequence, e := c.Store.NextControlSequence(ctx)
		if e != nil {
			return e
		}
		body.HubId, body.BootId, body.Sequence = c.HubID, c.BootID, sequence
		return stream.Send(body)
	}
	if e := send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: fundedCapabilities, SessionId: session, RegionId: c.Region}}}); e != nil {
		return e
	}
	// Exactly one goroutine receives; all sends and local confirmations remain
	// serialized here. A cancelled stream cannot strand a blocked sender.
	type incoming struct {
		message *controlv1.BackendMessage
		err     error
	}
	ch := make(chan incoming, 1)
	go func() {
		for {
			m, e := stream.Recv()
			select {
			case ch <- incoming{m, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last uint64
	ready := false
	var pending *store.LedgerRecord
	var installed *controlv1.LedgerRecord
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !ready || pending != nil {
				continue
			}
			records, e := c.Store.PendingLedger(ctx, 1000)
			if e != nil {
				return e
			}
			for _, r := range records {
				if r.Kind != "issue" {
					continue
				} // recovery/closure need separate consumers
				pending = &r
				if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: r.ID, LedgerSequence: uint64(r.Sequence), Kind: "issue-v1", Payload: r.Payload, PayloadSha256: r.Digest}}}); e != nil {
					return e
				}
				break
			}
		case received := <-ch:
			if received.err != nil {
				return received.err
			}
			m := received.message
			if m.MessageId == "" || m.Sequence <= last {
				return errors.New("backend funded envelope sequence invalid")
			}
			last = m.Sequence
			if hb := m.GetHeartbeat(); hb != nil {
				if e := c.persistFundedHeartbeat(ctx, session, hb); e != nil {
					return e
				}
				now := c.Clock()
				if !validFundedClock(now) {
					return store.ErrAuthorityTimeUncertain
				}
				snapshot, e := c.Store.HubControlSnapshot(ctx, c.HubID, now.Latest)
				if e != nil {
					return e
				}
				if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: c.HubID, RegionId: c.Region, SessionId: session, ControlVersion: "1.6.0", Capabilities: fundedCapabilities, Mode: snapshot.Mode, SignerEpoch: snapshot.Epoch, BackendHeartbeatAt: hb.ObservedAt, AutonomyDeadline: hb.AutonomyDeadline, ConfirmedSequence: snapshot.Confirmed, PendingTailRecords: snapshot.Pending}}}); e != nil {
					return e
				}
				ready = true
				continue
			}
			if !ready {
				return errors.New("funded heartbeat required first")
			}
			if allocation := m.GetAllocation(); allocation != nil {
				digest := sha256.Sum256(allocation.Payload)
				if !bytes.Equal(digest[:], allocation.PayloadSha256) {
					return ErrAllocation
				}
				if installed != nil {
					return errors.New("unconfirmed allocation receipt still pending")
				}
				a, e := DecodeAllocation(allocation.Payload)
				if e != nil {
					return e
				}
				if a.AllocationID != allocation.AllocationId || a.HubID != c.HubID || a.Region != c.Region {
					return ErrAllocation
				}
				if e = c.Store.InstallFundedHubDelegation(ctx, c.BackendID, a.Delegation(), c.Clock()); e != nil {
					return e
				}
				installed = &controlv1.LedgerRecord{RecordId: a.AllocationID, LedgerSequence: m.Sequence, Kind: "allocation-installed-v1", PayloadSha256: digest[:]}
				// Only the FULL commit above authorizes this receipt. A retry returns
				// the exact existing delegation, including its spent balance.
				if e = send(&controlv1.HubMessage{Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: a.AllocationID, LedgerSequence: m.Sequence, Kind: "allocation-installed-v1", Payload: allocation.Payload, PayloadSha256: digest[:]}}}); e != nil {
					return e
				}
				continue
			}
			if ack := m.GetLedgerAck(); ack != nil {
				if ack.Kind == "allocation-installed-v1" {
					if installed == nil || ack.RecordId != installed.RecordId || ack.LedgerSequence != installed.LedgerSequence || !bytes.Equal(ack.PayloadSha256, installed.PayloadSha256) {
						return errors.New("allocation acknowledgement mismatch")
					}
					installed = nil
					continue
				}
				if pending == nil || ack.Kind != "issue-v1" || ack.RecordId != pending.ID || ack.LedgerSequence != uint64(pending.Sequence) || !bytes.Equal(ack.PayloadSha256, pending.Digest) {
					return errors.New("funded journal acknowledgement mismatch")
				}
				if _, e := c.Store.ConfirmLedger(ctx, *pending, ack.PayloadSha256); e != nil {
					return e
				}
				pending = nil
				continue
			}
			return errors.New("unsupported funded backend body")
		}
	}
}

func validFundedClock(now store.IssuerTime) bool {
	return now.Trusted && !now.Earliest.IsZero() && !now.Latest.Before(now.Earliest) && now.Latest.Sub(now.Earliest) <= 10*time.Second
}

func (c *BackendClient) persistFundedHeartbeat(ctx context.Context, session string, hb *controlv1.BackendHeartbeat) error {
	if hb.BackendId != c.BackendID || hb.HubId != c.HubID || hb.SessionId != session || hb.RegionId != c.Region {
		return errors.New("funded heartbeat identity mismatch")
	}
	observed, e := time.Parse(time.RFC3339Nano, hb.ObservedAt)
	if e != nil {
		return e
	}
	deadline, e := time.Parse(time.RFC3339Nano, hb.AutonomyDeadline)
	if e != nil {
		return e
	}
	if !deadline.After(observed) || deadline.After(observed.Add(12*time.Hour)) {
		return store.ErrAuthorityTimeUncertain
	}
	// Independent bounded clocks can overlap without either lower bound proving
	// the other timestamp is already past. Wait for that proof; never widen time.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		now := c.Clock()
		if !validFundedClock(now) {
			return store.ErrAuthorityTimeUncertain
		}
		if !now.Earliest.Before(observed) {
			if !now.Latest.Before(observed.Add(5*time.Second)) || !now.Latest.Before(deadline) {
				return store.ErrAuthorityExpired
			}
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return store.ErrAuthorityTimeUncertain
		case <-time.After(25 * time.Millisecond):
		}
	}
	e = c.Store.RecordBackendHeartbeat(ctx, observed, deadline)
	if errors.Is(e, store.ErrHeartbeatStale) {
		now := c.Clock()
		status, readErr := c.Store.CurrentAutonomyStatus(ctx, now.Latest, now.Trusted)
		if readErr == nil && status.Mode == store.AutonomyOnline && status.BackendHeartbeat.Equal(observed) && status.HardDeadline.Equal(deadline) {
			return nil
		}
	}
	return e
}
