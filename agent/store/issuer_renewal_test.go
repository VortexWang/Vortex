package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// Roots enter through the trusted provisioning adapter, never a node request.
// This fixture does not stand in for authenticated backend provisioning.
func renewalIssuerFixture(t *testing.T) (*Store, HubDelegation, IssuerTime, string) {
	t.Helper()
	base, d, clock, _ := issuerFixture(t)
	_ = base
	path := filepath.Join(t.TempDir(), "renewal-hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
		r := ShareReference{ID: shareID(700 + i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: d.SignerEpoch, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: d.ValidFrom, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline}
		if err := s.InstallShareSourceAnchor(context.Background(), d.NodeID, shareID(799), r); err != nil {
			t.Fatal(err)
		}
		d.ShareReferences = append(d.ShareReferences, r)
	}
	if err := s.InstallHubDelegation(context.Background(), d, clock); err != nil {
		t.Fatal(err)
	}
	return s, d, clock, path
}

func sealedIssuerRenewal(t *testing.T, d HubDelegation, n int) IssueRequest {
	t.Helper()
	r := issuerRequest(d, n)
	wire := &controlv1.RenewalRequest{RequestId: r.ID, NodeId: d.NodeID, BootId: d.BootID, DelegationId: d.ID, SourceId: d.SourceID, RuleId: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, FencingGeneration: d.FencingFloor, SignerEpoch: d.SignerEpoch, BillingSnapshotId: d.BillingSnapshotID, RequestedRawBytes: r.RawBytes, RequestedTtlSeconds: uint64(r.TTL / time.Second)}
	var err error
	wire.RequestSha256, err = contracts.RenewalDigest(wire)
	if err != nil {
		t.Fatal(err)
	}
	r.RenewalPayload, err = contracts.EncodeRenewalRequest(wire)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIssuerRenewalCommitsExactResultAndReplaysAfterReopen(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	r := sealedIssuerRenewal(t, d, 800)
	got, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Version        string           `json:"schema_version"`
		RequestID      string           `json:"renewal_request_id"`
		RequestHash    string           `json:"renewal_request_sha256"`
		Refs           []ShareReference `json:"share_references"`
		Authorizations []struct {
			ShareID      string `json:"share_reference_id"`
			DelegationID string `json:"delegation_id"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	wire, _ := contracts.DecodeRenewalRequest(r.RenewalPayload)
	if payload.Version != "1.2.0" || payload.RequestID != r.ID || payload.RequestHash != hex.EncodeToString(wire.RequestSha256) || len(payload.Refs) != 3 || len(payload.Authorizations) != 3 || len(got.RenewalResult) == 0 {
		t.Fatalf("missing full correlated authority: %s", got.Payload)
	}
	for _, a := range payload.Authorizations {
		if a.ShareID == "" || a.DelegationID != d.ID {
			t.Fatal("unbound authorization")
		}
	}
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	replay, err := other.IssueHubAuthorization(ctx, r, clock)
	if err != nil || !bytes.Equal(got.Payload, replay.Payload) || !bytes.Equal(got.RenewalResult, replay.RenewalResult) {
		t.Fatalf("lost reply changed: %v", err)
	}
	bad := r
	bad.RawBytes++
	if _, err := other.IssueHubAuthorization(ctx, bad, clock); err == nil {
		t.Fatal("changed amount accepted")
	}
	b, err := other.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 256 || b.Remaining != 768 {
		t.Fatalf("duplicate debit: %+v %v", b, err)
	}
}

func TestIssuerRenewalRejectsStaleRootWithoutDebiting(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	root := d.ShareReferences[0]
	root.Revision++
	if err := s.InstallShareSourceAnchor(context.Background(), d.NodeID, shareID(799), root); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueHubAuthorization(context.Background(), sealedIssuerRenewal(t, d, 801), clock); err == nil {
		t.Fatal("obsolete root minted authority")
	}
	b, err := s.AuditHubDelegation(context.Background(), d.ID)
	if err != nil || b.Issued != 0 || b.Remaining != 1024 {
		t.Fatalf("stale root debit: %+v %v", b, err)
	}
}

func TestIssuerRenewalAtomicFailureAndConcurrentExhaustion(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_renewal BEFORE INSERT ON issuer_issues BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	r := sealedIssuerRenewal(t, d, 802)
	if _, err := s.IssueHubAuthorization(ctx, r, clock); err == nil {
		t.Fatal("failed durable result returned success")
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 1024 || b.Issued != 0 {
		t.Fatalf("partial debit: %+v %v", b, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_renewal`); err != nil {
		t.Fatal(err)
	}
	requests := make([]IssueRequest, 12)
	for i := range requests {
		requests[i] = sealedIssuerRenewal(t, d, 810+i)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for _, request := range requests {
		wg.Add(1)
		go func(r IssueRequest) {
			defer wg.Done()
			result, err := s.IssueHubAuthorization(ctx, r, clock)
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
				if len(result.RenewalResult) == 0 {
					t.Error("missing committed result")
				}
			}
		}(request)
	}
	wg.Wait()
	b, err = s.AuditHubDelegation(ctx, d.ID)
	if success != 4 || err != nil || b.Issued != 1024 || b.Remaining != 0 {
		t.Fatalf("exhaustion success=%d balance=%+v err=%v", success, b, err)
	}
}

func TestIssuerRenewalMigrationPreservesLegacyResult(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	r := issuerRequest(d, 850)
	before, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE issuer_issues DROP COLUMN renewal_result`,
		`ALTER TABLE authorization_records DROP COLUMN delegation_id`,
		`ALTER TABLE authorization_records DROP COLUMN share_reference_id`,
		`ALTER TABLE authorization_records DROP COLUMN share_revision`,
		`DROP TABLE share_reference_heads`, `DROP TABLE share_references`,
		`DROP TABLE share_source_heads`, `DROP TABLE share_source_anchors`,
		`UPDATE schema_meta SET value='14' WHERE key='schema_version'`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	after, err := migrated.IssueHubAuthorization(ctx, r, clock)
	if err != nil || !bytes.Equal(before.Payload, after.Payload) || len(after.RenewalResult) != 0 {
		t.Fatalf("legacy replay changed: %v", err)
	}
	var version string
	if err := migrated.db.QueryRow(`SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "15" {
		t.Fatalf("migration marker: %q %v", version, err)
	}
	b, err := migrated.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 768 || b.Issued != 256 {
		t.Fatalf("migration changed funds: %+v %v", b, err)
	}
}
