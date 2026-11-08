package store

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

func TestFundedBootstrapReplayReopenAndWriteFailureRetainOnePartition(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.db.Exec(`CREATE TRIGGER fail_bootstrap BEFORE INSERT ON issuer_issues BEGIN SELECT RAISE(ABORT,'bootstrap disk failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock); e == nil {
		t.Fatal("FULL failure returned authority")
	}
	b, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Issued != 0 || b.Remaining != d.RawBytes {
		t.Fatal("partial bootstrap", b, e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_bootstrap`); e != nil {
		t.Fatal(e)
	}
	page, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(page.Updates) != 1 || !page.Complete {
		t.Fatal(page, e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		p, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
		if e != nil || len(p.Updates) != 1 || !bytes.Equal(p.Updates[0].Payload, page.Updates[0].Payload) {
			t.Fatal("reconnect refilled or rebound", e)
		}
	}
	b, e = s.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Issued != d.RawBytes || b.Remaining != 0 {
		t.Fatal("bootstrap was not one finite partition", b, e)
	}
	if p, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, shareID(990), "", clock); e != nil || len(p.Updates) != 0 {
		t.Fatal("new boot selected old funding", e)
	}
	if _, e = s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", IssuerTime{}); e == nil {
		t.Fatal("untrusted bootstrap")
	}
	clock.Earliest, clock.Latest = d.ExpiresAt, d.ExpiresAt.Add(time.Second)
	if p, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock); e != nil || len(p.Updates) != 0 {
		t.Fatal("expired bootstrap replayed as new authority", e)
	}
	b, e = s.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Issued != d.RawBytes || b.Remaining != 0 {
		t.Fatal("expiry returned unknown bytes", b, e)
	}
}

func TestFundedBootstrapConcurrentStreamsSpendOnce(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	var wg sync.WaitGroup
	results := make(chan BootstrapPage, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := s.BootstrapHubAuthorizations(context.Background(), d.HubID, d.NodeID, d.BootID, "", clock)
			results <- p
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []byte
	for p := range results {
		if len(p.Updates) != 1 {
			t.Fatal(p)
		}
		if first == nil {
			first = p.Updates[0].Payload
		}
		if !bytes.Equal(first, p.Updates[0].Payload) {
			t.Fatal("racing streams received different funded issues")
		}
	}
	b, e := s.AuditHubDelegation(context.Background(), d.ID)
	if e != nil || b.Issued != d.RawBytes || b.Remaining != 0 {
		t.Fatal(b, e)
	}
}
