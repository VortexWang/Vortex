package store

import (
	"bytes"
	"context"
	"crypto/sha256"
)

// LedgerPrefix returns retained evidence including confirmed rows. It never
// changes ACKs, financial authority or retention. Pages are bounded by both
// count and four MiB; a single supported record is at most one MiB.
func (s *Store) LedgerPrefix(ctx context.Context, after, through int64, limit int) ([]LedgerRecord, error) {
	if s.owner != "hub" || after < 0 || through < after || limit < 1 || limit > 1000 {
		return nil, ErrAuthorizationDenied
	}
	rows, e := s.db.QueryContext(ctx, `SELECT sequence,kind,record_id,payload,payload_sha256 FROM hub_ledger WHERE sequence>? AND sequence<=? ORDER BY sequence LIMIT ?`, after, through, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []LedgerRecord{}
	total := 0
	next := after + 1
	for rows.Next() {
		var r LedgerRecord
		if e = rows.Scan(&r.Sequence, &r.Kind, &r.ID, &r.Payload, &r.Digest); e != nil {
			return nil, e
		}
		sum := sha256.Sum256(r.Payload)
		if r.Sequence != next || len(r.Payload) > 1<<20 || !bytes.Equal(sum[:], r.Digest) {
			return nil, ErrUsageBatchConflict
		}
		if len(out) > 0 && total+len(r.Payload) > 4<<20 {
			break
		}
		total += len(r.Payload)
		out = append(out, r)
		next++
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(out) == 0 && after < through {
		return nil, ErrUsageBatchConflict
	}
	return out, nil
}
