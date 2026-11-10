package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

func TestHubRuntimeLeaseAuditedIdempotentAndIndependentFromCredit(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now.Add(time.Second))
	initial, _, seq := issueAllocationFixture(t, x, hs, a, 51)
	when := x.now.Add(62 * time.Second)
	for _, seconds := range []int{20, 40, 60, 62} {
		if e = hs.RecordBackendHeartbeat(ctx, x.now.Add(time.Duration(seconds)*time.Second), a.AutonomyDeadline); e != nil {
			t.Fatal(e)
		}
	}
	lease, e := hs.RefreshHubDelegationLeases(ctx, in.HubID, in.NodeID, in.BootID, a.DelegationID, store.IssuerTime{Earliest: when, Latest: when, Trusted: true})
	if e != nil || len(lease.Payload) == 0 {
		t.Fatal(e)
	}
	rows, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(rows) != 2 {
		t.Fatal(rows, e)
	}
	leaseSeq := rows[1].Sequence
	financial := []string{"reservations", "ledger_events", "ledger_projections", "hub_credit_grants", "hub_issue_receipts"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if e = x.s.RecordHubRuntimeLease(x.f.contexts[in.HubID], in.HubID, in.HubBootID, leaseSeq, lease.Payload, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("accepted before initial issue registration", e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, initial.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if _, e = x.f.pool.Exec(ctx, `CREATE FUNCTION reject_runtime_lease() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='hub.runtime_lease' THEN RAISE EXCEPTION 'injected lease audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_runtime_lease BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_runtime_lease()`); e != nil {
		t.Fatal(e)
	}
	atomicRows := append(append([]string{}, financial...), "hub_runtime_lease_receipts", "tasks", "operations", "outbox")
	atomicBefore := authoritySnapshot(t, x.f.pool, x.f.tenant, atomicRows)
	if e = x.s.RecordHubRuntimeLease(x.f.contexts[in.HubID], in.HubID, in.HubBootID, leaseSeq, lease.Payload, uuid.NewString()); e == nil {
		t.Fatal("lease escaped audit")
	}
	if !bytes.Equal(atomicBefore, authoritySnapshot(t, x.f.pool, x.f.tenant, atomicRows)) {
		t.Fatal("partial lease receipt transaction")
	}
	if _, e = x.f.pool.Exec(ctx, `DROP TRIGGER reject_runtime_lease ON audit_events;DROP FUNCTION reject_runtime_lease()`); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if e := x.s.RecordHubRuntimeLease(x.f.contexts[in.HubID], in.HubID, in.HubBootID, leaseSeq, lease.Payload, uuid.NewString()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("lease created credit or changed financial authority")
	}
	var count int
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM hub_runtime_lease_receipts WHERE tenant_id=$1`, x.f.tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	q := generated.New(x.f.pool)
	var head struct {
		ID string `json:"update_id"`
	}
	if e = json.Unmarshal(lease.Payload, &head); e != nil {
		t.Fatal(e)
	}
	receipt, e := q.GetHubRuntimeLease(ctx, generated.GetHubRuntimeLeaseParams{TenantID: x.f.tenant, IssueID: head.ID})
	if e != nil || !bytes.Equal(receipt.Payload, lease.Payload) || len(receipt.PeerFingerprint) != 32 {
		t.Fatal(receipt, e)
	}
	heartbeat, e := hs.RefreshHubRuntimeHeartbeat(ctx, in.HubID, in.NodeID, in.BootID, a.DelegationID, store.IssuerTime{Earliest: when, Latest: when, Trusted: true})
	if e != nil || len(heartbeat.Payload) == 0 {
		t.Fatal("missing independent heartbeat", e)
	}
	// A fresh ID cannot bypass either independent cadence. The first heartbeat
	// is measured from the initial issue, and heartbeats do not postpone leases.
	for _, raw := range [][]byte{lease.Payload, heartbeat.Payload} {
		var v map[string]any
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		v["update_id"] = uuid.NewString()
		tooSoon := when
		if len(v["authorizations"].([]any)) == 0 {
			var initialEnvelope struct {
				IssuedAt time.Time `json:"issued_at"`
			}
			if e = json.Unmarshal(initial.Payload, &initialEnvelope); e != nil {
				t.Fatal(e)
			}
			tooSoon = initialEnvelope.IssuedAt.Add(9 * time.Second)
			v["issued_at"] = tooSoon.Format(time.RFC3339Nano)
			v["backend_heartbeat_at"] = tooSoon.Format(time.RFC3339Nano)
		}
		changed, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = x.s.RecordHubRuntimeLease(x.f.contexts[in.HubID], in.HubID, in.HubBootID, leaseSeq+1, changed, uuid.NewString()); !errors.Is(err, service.ErrLeaseBoundary) {
			t.Fatal("fresh ID bypassed independent cadence", tooSoon, err)
		}
	}
	if e = x.s.RecordHubRuntimeLease(x.f.contexts[in.HubID], in.HubID, in.HubBootID, leaseSeq+1, heartbeat.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("heartbeat changed financial authority")
	}
	for _, negative := range []string{"unsigned", "boot", "sequence", "credit", "source", "limits", "revocation", "renewal"} {
		t.Run(negative, func(t *testing.T) {
			c, boot, seq, raw := x.f.contexts[in.HubID], in.HubBootID, leaseSeq, lease.Payload
			switch negative {
			case "unsigned":
				c = ctx
			case "boot":
				boot = uuid.NewString()
			case "sequence":
				seq++
			case "credit":
				raw = initial.Payload
			default:
				var v map[string]any
				if e = json.Unmarshal(raw, &v); e != nil {
					t.Fatal(e)
				}
				auth := v["authorizations"].([]any)
				switch negative {
				case "source":
					auth[0].(map[string]any)["source_id"] = uuid.NewString()
				case "limits":
					for _, r := range auth {
						m := r.(map[string]any)
						if m["kind"] == "share_lease" {
							m["rate_bps"] = a.RateBPS + 1
						}
					}
				case "revocation":
					v["revocations"] = []string{uuid.NewString()}
				case "renewal":
					v["renewal_request_id"] = uuid.NewString()
					sum := sha256.Sum256([]byte("unsolicited"))
					v["renewal_request_sha256"] = hex.EncodeToString(sum[:])
				}
				raw, e = json.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
			}
			if e = x.s.RecordHubRuntimeLease(c, in.HubID, boot, seq, raw, uuid.NewString()); e == nil {
				t.Fatal("accepted invalid runtime lease", negative)
			}
		})
	}
}
