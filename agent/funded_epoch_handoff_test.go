package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// Real Hub/Node mTLS exercises funded1.2 decoding and FULL source continuity.
// Backend provisioning/ACKs here are explicit internal fixtures, not PG proof.
func TestFundedHubEpochHandoffMutualTLSKeepsOriginalSourcesAndDebt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	ns, err := store.Open(filepath.Join(dir, "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	n, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := func(i int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i) }
	now := time.Now().UTC()
	d := store.HubDelegation{ID: id(1800), HubID: regionalHubID, NodeID: n.NodeID, BootID: n.BootID, SourceID: id(1801), RuntimeSourceID: id(1802), ShareSourceID: id(1803), BillingSnapshotID: id(1804), RuleID: "funded-rule", RuleRevision: 2, PathGeneration: 4, FencingFloor: 8, SignerEpoch: 1, RawBytes: 1024, RateBPS: 1000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
		r := store.ShareReference{ID: id(1810 + i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: d.SignerEpoch, Generation: 7, Revision: 3, RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: now, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline}
		for _, s := range []*store.Store{ns, hs} {
			if err = s.InstallShareSourceAnchor(ctx, d.NodeID, id(1819), r); err != nil {
				t.Fatal(err)
			}
		}
		d.ShareReferences = append(d.ShareReferences, r)
	}
	clock := func() store.IssuerTime {
		v := time.Now().UTC()
		return store.IssuerTime{Earliest: v, Latest: v, Trusted: true}
	}
	if err = hs.InstallHubDelegation(ctx, d, clock()); err != nil {
		t.Fatal(err)
	}
	issuer := &hub.Issuer{Store: hs, HubID: d.HubID, Clock: clock}
	first, err := issuer.Issue(ctx, d.ID, d.NodeID, d.BootID, id(1820), 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS, _ := controlTLSWithHub(t, d.NodeID, d.HubID)
	client := &ControlClient{Node: n, TLS: clientTLS}
	deliver := func(update *controlv1.AuthorizationUpdate) {
		t.Helper()
		ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(node, boot string) (*controlv1.AuthorizationUpdate, error) { return update, nil }}, serverTLS)
		if err := client.Connect(ctx, ln.Addr().String()); err != nil {
			t.Fatal(err)
		}
	}
	deliver(first)
	p, err := validateAuthorizationUpdate(first.Payload, d.NodeID, d.BootID, d.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	var grant string
	for _, a := range p.Authorizations {
		if a.Kind == "credit_grant" {
			grant = a.AuthorizationID
		}
	}
	for {
		lo, _, ok := n.TrustedTime()
		if ok && !lo.Before(p.IssuedAt) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	lo, hi, trusted := n.TrustedTime()
	use := store.AuthorizationUse{NodeID: d.NodeID, BootID: d.BootID, AuthorizationID: grant, Kind: "credit_grant", RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, BillingSnapshotID: d.BillingSnapshotID, Earliest: lo, Latest: hi, TrustedTime: trusted}
	reservation := store.UsageReservation{NodeID: d.NodeID, BootID: d.BootID, AuthorizationID: grant, BatchID: "held-before-funded-handoff", SnapshotID: d.BillingSnapshotID, FlowKey: "old", RuleID: d.RuleID, Direction: "egress", Reserved: 64}
	if _, err = ns.ReserveAuthorizedUsage(ctx, reservation, use); err != nil {
		t.Fatal(err)
	}
	unknown, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: reservation.BatchID, OriginBootId: d.BootID, BillingSnapshotId: d.BillingSnapshotID, GrantId: grant, RuleId: d.RuleID, Role: "source", Direction: "egress", UnknownUpperBound: 64})
	if err != nil {
		t.Fatal(err)
	}
	if err = ns.SettleUnknownUsageReservation(ctx, reservation, unknown); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	digest := sha256.Sum256(raw)
	roots := append([]store.ShareReference(nil), d.ShareReferences...)
	for i := range roots {
		roots[i].SignerEpoch++
		roots[i].Revision++
	}
	h := store.HandoffRequest{ID: id(1821), HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: time.Now().UTC(), BackendID: id(1819), FundedSuccessors: []store.FundedHandoffSuccessor{{DelegationID: d.ID, AllocationSHA256: hex.EncodeToString(digest[:]), ShareReferences: roots}}}
	result, err := hs.HandoffHub(ctx, h, clock())
	if err != nil || result.Remaining[d.ID] != 768 {
		t.Fatalf("handoff %+v %v", result, err)
	}
	if _, err = issuer.Issue(ctx, d.ID, d.NodeID, d.BootID, id(1822), 256, time.Minute); err == nil {
		t.Fatal("new credit before complete prefix")
	}
	records, err := hs.PendingLedger(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if _, err = hs.ConfirmLedger(ctx, r, r.Digest); err != nil {
			t.Fatal(err)
		}
	}
	if done, err := hs.CompleteRecovery(ctx, clock()); err != nil || !done {
		t.Fatalf("completion %v %v", done, err)
	}
	// Direct backend root provisioning is a separate trusted fixture; the Hub
	// authorization stream cannot install its own backend anchor.
	for _, r := range roots {
		if err = ns.InstallShareSourceAnchor(ctx, d.NodeID, h.BackendID, r); err != nil {
			t.Fatal(err)
		}
	}
	recovery, err := issuer.RecoveryUpdate(ctx, h.ID, d.NodeID, d.BootID, d.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	deliver(recovery)
	next, err := issuer.Issue(ctx, d.ID, d.NodeID, d.BootID, id(1822), 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	deliver(next)
	deliver(next) // FULL replay cannot consume R twice.
	parsed, err := validateAuthorizationUpdate(next.Payload, d.NodeID, d.BootID, d.PathGeneration)
	if err != nil || parsed.SchemaVersion != "1.2.0" || len(parsed.Handoffs) != 3 || parsed.SignerEpoch != 2 {
		t.Fatalf("funded payload %+v %v", parsed, err)
	}
	for _, r := range parsed.ShareReferences {
		if r.Revision != 4 || r.Generation != 7 || r.SignerEpoch != 2 || r.PathGeneration != 4 {
			t.Fatalf("merged generations %+v", r)
		}
	}
	balance, err := ns.AuditAuthorizationBalance(ctx, d.NodeID, grant)
	if err != nil || balance.Unknown != 64 {
		t.Fatalf("old debt %+v %v", balance, err)
	}
	// Old immutable rights still exist for draining; current admission uses new
	// roots. This check explicitly requests the original already-open session.
	oldRecord, err := ns.CheckStoredAuthorization(ctx, func() store.AuthorizationUse { v := use; v.ExistingSession = true; return v }())
	if err != nil || oldRecord.SignerEpoch != 1 {
		t.Fatalf("old grant lost %+v %v", oldRecord, err)
	}
	for _, a := range parsed.Authorizations {
		if a.Kind == "credit_grant" {
			use.AuthorizationID = a.AuthorizationID
		}
	}
	for {
		lo, _, ok := n.TrustedTime()
		if ok && !lo.Before(parsed.IssuedAt) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	use.Earliest, use.Latest, use.TrustedTime = n.TrustedTime()
	if _, err = ns.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("new funded credit denied", err)
	}
	b, err := hs.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 512 || b.Remaining != 512 {
		t.Fatalf("Hub R refilled %+v %v", b, err)
	}
	replay, err := issuer.Issue(ctx, d.ID, d.NodeID, d.BootID, id(1820), 256, time.Minute)
	if err != nil || !bytes.Equal(replay.Payload, first.Payload) {
		t.Fatal("old bytes changed", err)
	}
}
