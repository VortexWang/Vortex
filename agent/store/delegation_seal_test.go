package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
	"vortex/contracts"
)

func TestDelegationSealFULLRollbackInventoryRestartAndNoRefill(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	boot := "550e8400-e29b-41d4-a716-446655440911"
	req := issuerRequest(d, 200)
	first, e := s.IssueHubAuthorization(ctx, req, clock)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER fail_seal BEFORE INSERT ON funded_delegation_seals BEGIN SELECT RAISE(ABORT,'disk write failure'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SealHubDelegation(ctx, d.HubID, boot, d.ID); e == nil {
		t.Fatal("failed seal produced receipt")
	}
	var state string
	if e = s.db.QueryRow(`SELECT state FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&state); e != nil || state != "open" {
		t.Fatal(e, state)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_seal`); e != nil {
		t.Fatal(e)
	}
	sealed, e := s.SealHubDelegation(ctx, d.HubID, boot, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if sealed.Issued != 256 || sealed.Unissued != 768 || sealed.IssueCount != 1 {
		t.Fatal(sealed)
	}
	inv := []contracts.IssuedPartition{{ID: req.ID, Digest: contracts.SharePayloadDigest(first.Payload), Raw: 256}}
	digest, total, e := contracts.PartitionDigest(inv)
	if e != nil || digest != sealed.IssueDigest || total != sealed.Issued {
		t.Fatal(e, sealed)
	}
	if _, e = s.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock); e == nil {
		t.Fatal("sealed delegation issued new child")
	}
	second, e := s.IssueHubAuthorization(ctx, req, clock)
	if e != nil || !bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("prior issue replay", e)
	}
	reopened, e := Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	replay, e := reopened.SealHubDelegation(ctx, d.HubID, boot, d.ID)
	if e != nil || replay != sealed {
		t.Fatal("restart changed sealed inventory", e)
	}
	if _, e = reopened.SealHubDelegation(ctx, d.HubID, d.NodeID, d.ID); e == nil {
		t.Fatal("boot rebound")
	}
	records, e := reopened.PendingLedger(ctx, 100)
	if e != nil || len(records) != 2 || records[1].Kind != "delegation-seal" {
		t.Fatal(e, records)
	}
	raw, _ := json.Marshal(sealed)
	if !bytes.Equal(raw, records[1].Payload) {
		t.Fatal("journal changed seal")
	}
}
func TestDelegationSealExpiryNeverMeansChildStop(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	boot := "550e8400-e29b-41d4-a716-446655440911"
	if _, e := s.IssueHubAuthorization(ctx, issuerRequest(d, 200), clock); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.SealExpiredHubDelegations(ctx, d.HubID, boot, "", clock); e != nil {
		t.Fatal(e)
	}
	var count int
	if e := s.db.QueryRow(`SELECT count(*) FROM funded_delegation_seals`).Scan(&count); e != nil || count != 0 {
		t.Fatal("early sealed", e, count)
	}
	clock.Earliest = d.ExpiresAt.Add(time.Second)
	clock.Latest = clock.Earliest
	if _, _, e := s.SealExpiredHubDelegations(ctx, d.HubID, boot, "", clock); e != nil {
		t.Fatal(e)
	}
	b, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Issued != 256 || b.Remaining != 768 {
		t.Fatal("expiry changed issued debt", e, b)
	}
}

func TestDelegationSealConcurrentIssueCannotSpendReturnedPartition(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	boot := "550e8400-e29b-41d4-a716-446655440911"
	start := make(chan struct{})
	issueResult := make(chan error, 1)
	sealResult := make(chan error, 1)
	req := issuerRequest(d, 202)
	go func() {
		<-start
		_, err := s.IssueHubAuthorization(ctx, req, clock)
		issueResult <- err
	}()
	go func() {
		<-start
		_, err := s.SealHubDelegation(ctx, d.HubID, boot, d.ID)
		sealResult <- err
	}()
	close(start)
	issueErr := <-issueResult
	<-sealResult // SQLite may reject a racing writer; no receipt is assumed.
	seal, err := s.SealHubDelegation(ctx, d.HubID, boot, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantIssued := int64(0)
	if issueErr == nil {
		wantIssued = 256
		if _, err = s.IssueHubAuthorization(ctx, req, clock); err != nil {
			t.Fatal("committed pre-seal request cannot replay", err)
		}
	}
	if seal.Issued != wantIssued || seal.Unissued != 1024-wantIssued {
		t.Fatal("seal omitted a committed concurrent issue", issueErr, seal)
	}
	if _, err = s.IssueHubAuthorization(ctx, issuerRequest(d, 203), clock); err == nil {
		t.Fatal("new issue spent the sealed partition")
	}
}
