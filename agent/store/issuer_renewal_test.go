package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
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

func TestIssuerRenewalMigrationFailureRollsBackSchemaAndFunds(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	r := issuerRequest(d, 950)
	before, err := s.IssueHubAuthorization(context.Background(), r, clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`DROP TABLE issuer_renewals`, `DROP INDEX renewal_trigger_once`, `ALTER TABLE renewal_requests DROP COLUMN trigger_authorization_id`, `UPDATE schema_meta SET value='15' WHERE key='schema_version'`, `CREATE TRIGGER fail_migration BEFORE UPDATE ON schema_meta WHEN NEW.value='16' BEGIN SELECT RAISE(ABORT,'disk failure'); END`} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if attempted, err := Open(path, "hub"); err == nil {
		attempted.Close()
		t.Fatal("failed migration opened authority writer")
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version string
	if err := raw.QueryRow(`SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "15" {
		t.Fatalf("failed migration changed completion marker %q %v", version, err)
	}
	var count int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='issuer_renewals'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial table survived %d %v", count, err)
	}
	if err := raw.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('renewal_requests') WHERE name='trigger_authorization_id'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial column survived %d %v", count, err)
	}
	if _, err := raw.Exec(`DROP TRIGGER fail_migration`); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	after, err := migrated.IssueHubAuthorization(context.Background(), r, clock)
	if err != nil || !bytes.Equal(before.Payload, after.Payload) {
		t.Fatalf("retry changed legacy payload: %v", err)
	}
	balance, err := migrated.AuditHubDelegation(context.Background(), d.ID)
	if err != nil || balance.Issued != 256 || balance.Remaining != 768 {
		t.Fatalf("migration altered funds %+v %v", balance, err)
	}
}

func TestIssuerRenewalTerminalRejectionSurvivesReopenAndProvisioning(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	r := sealedIssuerRenewal(t, d, 900)
	wire, _ := contracts.DecodeRenewalRequest(r.RenewalPayload)
	uncertain := clock
	uncertain.Trusted = false
	first, err := s.RenewHubAuthorization(ctx, d.HubID, wire, uncertain)
	if err != nil || first.Code != "time_uncertain" || first.AuthorizationUpdate != nil {
		t.Fatalf("refusal=%v %v", first, err)
	}
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	replay, err := other.RenewHubAuthorization(ctx, d.HubID, wire, clock)
	if err != nil || !proto.Equal(first, replay) {
		t.Fatalf("refusal became authority: %v %v", replay, err)
	}
	if _, err := other.IssueHubAuthorization(ctx, r, clock); err == nil {
		t.Fatal("direct issuance bypassed terminal refusal")
	}
	legacy := r
	legacy.RenewalPayload = nil
	if _, err := other.IssueHubAuthorization(ctx, legacy, clock); err == nil {
		t.Fatal("legacy issuance bypassed terminal refusal")
	}
	changed := proto.Clone(wire).(*controlv1.RenewalRequest)
	changed.RequestedRawBytes++
	changed.RequestSha256, _ = contracts.RenewalDigest(changed)
	if _, err := other.RenewHubAuthorization(ctx, d.HubID, changed, clock); err == nil {
		t.Fatal("changed refused request accepted")
	}
	b, err := other.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 0 || b.Remaining != 1024 {
		t.Fatalf("refusal altered funding: %+v %v", b, err)
	}
}

func TestIssuerRenewalTerminalOutcomeAtomicWithDebit(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	r := sealedIssuerRenewal(t, d, 901)
	wire, _ := contracts.DecodeRenewalRequest(r.RenewalPayload)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_terminal BEFORE INSERT ON issuer_renewals BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if result, err := s.RenewHubAuthorization(ctx, d.HubID, wire, clock); err == nil || result != nil {
		t.Fatal("uncommitted terminal result escaped")
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 0 || b.Remaining != 1024 {
		t.Fatalf("terminal failure left debit: %+v %v", b, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_terminal`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan *controlv1.RenewalResult, 12)
	for j := 0; j < 12; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.RenewHubAuthorization(ctx, d.HubID, wire, clock)
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	var first *controlv1.RenewalResult
	count := 0
	for result := range results {
		count++
		if first == nil {
			first = result
		}
		if !proto.Equal(first, result) {
			t.Fatal("concurrent replay changed result")
		}
	}
	if count != 12 || first.Code != "ok" {
		t.Fatalf("results=%d first=%v", count, first)
	}
	other, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	unknown := IssuerTime{}
	replay, err := other.RenewHubAuthorization(ctx, d.HubID, wire, unknown)
	if err != nil || !proto.Equal(first, replay) {
		t.Fatalf("durable success lost after clock reset: %v", err)
	}
	b, err = other.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 256 || b.Remaining != 768 {
		t.Fatalf("duplicate debit %+v %v", b, err)
	}
}

func TestIssuerRenewalQuotaRejectionIsDurable(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	for j := 0; j < 5; j++ {
		r := sealedIssuerRenewal(t, d, 910+j)
		wire, _ := contracts.DecodeRenewalRequest(r.RenewalPayload)
		result, err := s.RenewHubAuthorization(ctx, d.HubID, wire, clock)
		want := "ok"
		if j == 4 {
			want = "quota_exhausted"
		}
		if err != nil || result.Code != want {
			t.Fatalf("result %d=%v %v", j, result, err)
		}
		if j == 4 {
			replay, err := s.RenewHubAuthorization(ctx, d.HubID, wire, IssuerTime{})
			if err != nil || !proto.Equal(result, replay) {
				t.Fatalf("quota replay %v", err)
			}
		}
	}
	b, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Issued != 1024 || b.Remaining != 0 {
		t.Fatalf("exhaustion %+v %v", b, err)
	}
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
		`DROP TABLE issuer_renewals`,
		`DROP INDEX renewal_trigger_once`,
		`ALTER TABLE renewal_requests DROP COLUMN trigger_authorization_id`,
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
	if err := migrated.db.QueryRow(`SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "16" {
		t.Fatalf("migration marker: %q %v", version, err)
	}
	b, err := migrated.AuditHubDelegation(ctx, d.ID)
	if err != nil || b.Remaining != 768 || b.Issued != 256 {
		t.Fatalf("migration changed funds: %+v %v", b, err)
	}
}
