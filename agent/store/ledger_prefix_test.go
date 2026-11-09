package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
)

func TestLedgerPrefixPagesIncludeConfirmedEvidence(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		r := issuerRequest(d, 900+i)
		r.RawBytes = 1
		if _, e := s.IssueHubAuthorization(ctx, r, clock); e != nil {
			t.Fatal(e)
		}
	}
	pending, e := s.PendingLedger(ctx, 1000)
	if e != nil || len(pending) != 40 {
		t.Fatal(len(pending), e)
	}
	if _, e = s.ConfirmLedger(ctx, pending[0], pending[0].Digest); e != nil {
		t.Fatal(e)
	}
	after := int64(0)
	count := 0
	for after < 40 {
		page, e := s.LedgerPrefix(ctx, after, 40, 7)
		if e != nil || len(page) == 0 || len(page) > 7 {
			t.Fatal(page, e)
		}
		for _, r := range page {
			if r.Sequence != int64(count+1) || !bytes.Equal(r.Payload, pending[count].Payload) {
				t.Fatal("page changed retained identity")
			}
			count++
			after = r.Sequence
		}
	}
	if count != 40 {
		t.Fatal(count)
	}
	if page, e := s.LedgerPrefix(ctx, 40, 40, 7); e != nil || len(page) != 0 {
		t.Fatal(page, e)
	}
	for _, v := range [][3]int64{{-1, 40, 7}, {41, 40, 7}, {0, 40, 0}, {0, 40, 1001}} {
		if _, e := s.LedgerPrefix(ctx, v[0], v[1], int(v[2])); e == nil {
			t.Fatal("invalid page request", v)
		}
	}
	if _, e = s.db.ExecContext(ctx, `DELETE FROM hub_ledger WHERE sequence=2`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LedgerPrefix(ctx, 0, 40, 7); e == nil {
		t.Fatal("prefix hole silently skipped")
	}
}

func TestLedgerPrefixByteBoundAndPayloadHash(t *testing.T) {
	s, _, _, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	raw := bytes.Repeat([]byte("x"), 1<<20)
	h := sha256.Sum256(raw)
	for i := 0; i < 6; i++ {
		if _, e := s.db.ExecContext(ctx, `INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256) VALUES('known',?,?,?)`, shareID(1100+i), raw, h[:]); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.LedgerPrefix(ctx, 0, 6, 1000)
	if e != nil || len(page) != 4 {
		t.Fatal("page exceeded byte budget", len(page), e)
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE hub_ledger SET payload_sha256=zeroblob(32) WHERE sequence=5`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LedgerPrefix(ctx, 4, 6, 1000); e == nil {
		t.Fatal("corrupt payload accepted")
	}
}
