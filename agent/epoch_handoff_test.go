package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestAuthenticatedHubEpochHandoffKeepsOldGrantAndBoundsSuccessor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	now := time.Now().UTC()
	d := store.HubDelegation{ID: "550e8400-e29b-41d4-a716-446655440022", HubID: "550e8400-e29b-41d4-a716-446655440060", SourceID: "550e8400-e29b-41d4-a716-446655440023", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440061", ShareSourceID: "550e8400-e29b-41d4-a716-446655440062", NodeID: node.NodeID, BootID: node.BootID, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024", RuleID: "rule", RuleRevision: 2, PathGeneration: 4, FencingFloor: 9, SignerEpoch: 1, RawBytes: 12288, RateBPS: 1000000, Connections: 1, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	clock := func() store.IssuerTime {
		n := time.Now().UTC()
		return store.IssuerTime{Earliest: n, Latest: n, Trusted: true}
	}
	if err = hs.InstallHubDelegation(ctx, d, clock()); err != nil {
		t.Fatal(err)
	}
	issuer := &hub.Issuer{Store: hs, HubID: d.HubID, Clock: clock}
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	issueID := "550e8400-e29b-41d4-a716-446655440020"
	ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
		result, e := issuer.Issue(ctx, d.ID, id, boot, issueID, 4096, time.Minute)
		return result, e
	}}, serverTLS)
	client := &ControlClient{Node: node, TLS: clientTLS}
	if err = client.Connect(ctx, ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
	last, err := issuer.Issue(ctx, d.ID, node.NodeID, node.BootID, issueID, 4096, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	firstPayload, err := validateAuthorizationUpdate(last.Payload, node.NodeID, node.BootID, 4)
	if err != nil {
		t.Fatal(err)
	}
	var old string
	for _, a := range firstPayload.Authorizations {
		if a.Kind == "credit_grant" {
			old = a.AuthorizationID
		}
	}
	for {
		low, _, ok := node.TrustedTime()
		if ok && !low.Before(firstPayload.IssuedAt) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	low, high, trusted := node.TrustedTime()
	use := store.AuthorizationUse{NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: old, Kind: "credit_grant", RuleID: d.RuleID, RuleRevision: 2, PathGeneration: 4, BillingSnapshotID: d.BillingSnapshotID, Earliest: low, Latest: high, TrustedTime: trusted}
	reservation := store.UsageReservation{NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: old, BatchID: "held-old", SnapshotID: d.BillingSnapshotID, FlowKey: "old", RuleID: d.RuleID, Direction: "egress", Reserved: 512}
	if _, err = s.ReserveAuthorizedUsage(ctx, reservation, use); err != nil {
		t.Fatal(err)
	}
	unknownRaw, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: reservation.BatchID, OriginBootId: node.BootID, BillingSnapshotId: d.BillingSnapshotID, GrantId: old, RuleId: d.RuleID, Role: "source", Direction: "egress", UnknownUpperBound: 512})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SettleUnknownUsageReservation(ctx, reservation, unknownRaw); err != nil {
		t.Fatal(err)
	}
	h := store.HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: time.Now().UTC()}
	result, err := hs.HandoffHub(ctx, h, clock())
	if err != nil || result.Remaining[d.ID] != 8192 {
		t.Fatalf("handoff %+v %v", result, err)
	}
	next, err := issuer.Issue(ctx, d.ID, node.NodeID, node.BootID, "550e8400-e29b-41d4-a716-446655440025", 4096, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// New listener uses the same authenticated Hub certificate identity. Successor
	// payload is delivered by real mTLS gRPC, never by a node-provided adapter.
	nextLn := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AcceptUsage: true, UsageBatchLimit: 1, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) { return next, nil }}, serverTLS)
	if err = client.Connect(ctx, nextLn.Addr().String()); err != nil {
		t.Fatal(err)
	}
	parsed, err := validateAuthorizationUpdate(next.Payload, node.NodeID, node.BootID, 4)
	if err != nil || parsed.SchemaVersion != "1.1.0" || len(parsed.Handoffs) != 1 {
		t.Fatalf("payload %+v %v", parsed, err)
	}
	if _, err = s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("old grant invalidated", err)
	}
	balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, old)
	if err != nil || balance.Unknown != 512 {
		t.Fatalf("debt %+v %v", balance, err)
	}
	// The same successor payload replays without spending its transfer twice.
	oldRecord, err := s.CheckStoredAuthorization(ctx, use)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.applyAuthorizationUpdateFrom(ctx, next, oldRecord.IssuerID); err != nil {
		t.Fatal(err)
	}
	for _, a := range parsed.Authorizations {
		if a.Kind == "credit_grant" {
			use.AuthorizationID = a.AuthorizationID
		}
	}
	for {
		low, _, ok := node.TrustedTime()
		if ok && !low.Before(parsed.IssuedAt) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	use.Earliest, use.Latest, use.TrustedTime = node.TrustedTime()
	if _, err = s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("successor denied", err)
	}
	// Contract decoder rejects handoffs smuggled in a legacy envelope or null.
	var body map[string]any
	if err = json.Unmarshal(next.Payload, &body); err != nil {
		t.Fatal(err)
	}
	body["schema_version"] = "1.0.0"
	bad, _ := json.Marshal(body)
	if _, err = validateAuthorizationUpdate(bad, node.NodeID, node.BootID, 4); err == nil {
		t.Fatal("legacy handoff accepted")
	}
	// Revoke every delegation during the next handoff. Recovery must still
	// deliver revocations even though issuing a successor grant is impossible.
	revoke := store.HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440801", HubID: d.HubID, FromEpoch: 2, ToEpoch: 3, Heartbeat: time.Now().UTC(), RevokeDelegations: []string{d.ID}}
	if _, err = hs.HandoffHub(ctx, revoke, clock()); err != nil {
		t.Fatal(err)
	}
	revokeUpdate, err := issuer.RecoveryUpdate(ctx, revoke.ID, node.NodeID, node.BootID, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Replay the outbox after materialization: content must be byte-identical.
	replayUpdate, err := issuer.RecoveryUpdate(ctx, revoke.ID, node.NodeID, node.BootID, 4)
	if err != nil || !bytes.Equal(revokeUpdate.Payload, replayUpdate.Payload) {
		t.Fatal("recovery outbox changed", err)
	}
	revokeLn := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) { return revokeUpdate, nil }}, serverTLS)
	if err = client.Connect(ctx, revokeLn.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CheckStoredAuthorization(ctx, use); err == nil {
		t.Fatal("revocation did not stop successor authorization")
	}
	balance, err = s.AuditAuthorizationBalance(ctx, node.NodeID, old)
	if err != nil || balance.Unknown != 512 {
		t.Fatalf("revocation released debt %+v %v", balance, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = hs.Close(); err != nil {
		t.Fatal(err)
	}
	saveLedgerFiles(t, "epoch", map[string]string{"agent": filepath.Join(dir, "agent.db"), "hub": filepath.Join(dir, "hub.db")})
}
