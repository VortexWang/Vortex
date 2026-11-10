package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The input contains at most 32 bound identities. Reads still compare every
// immutable payload; batching changes SQL round trips, never receipt meaning.
var knownUsageSetQueries = usageIdentityQueries(`SELECT 0,t.node_id,t.batch_id,t.snapshot_id,NULL,t.payload_sha256,'0','',NULL,NULL FROM settled_usage_tombstones t JOIN wanted w ON w.node_id=t.node_id AND w.batch_id=t.batch_id
UNION ALL SELECT 1,u.node_id,u.batch_id,u.snapshot_id,u.payload,u.payload_sha256,u.unknown_upper_bound,u.state,r.kind,r.payload_sha256 FROM usage_batches u JOIN wanted w ON w.node_id=u.node_id AND w.batch_id=u.batch_id LEFT JOIN usage_backend_receipts r ON r.node_id=u.node_id AND r.batch_id=u.batch_id
UNION ALL SELECT 2,u.node_id,u.batch_id,'',NULL,NULL,'0','',NULL,NULL FROM unknown_usage_batches u JOIN wanted w ON w.node_id=u.node_id AND w.batch_id=u.batch_id`)

const usageSetLedgeredQuery = `UPDATE usage_batches SET state='ledgered',updated_at=? WHERE (node_id,batch_id) IN (SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(?))`

var usageGroupConfirmedQueries = usageIdentityQueries(`SELECT 0,r.node_id,r.batch_id,r.kind,r.payload_sha256 FROM usage_backend_receipts r JOIN wanted w ON w.node_id=r.node_id AND w.batch_id=r.batch_id
UNION ALL SELECT 1,t.node_id,t.batch_id,'known',t.payload_sha256 FROM settled_usage_tombstones t JOIN wanted w ON w.node_id=t.node_id AND w.batch_id=t.batch_id`)

// Four fixed, prepared shapes avoid compiling a new multi-row statement on
// every replay. Empty slots bind NULL; actual payloads remain bound blobs.
var knownUsageInsertSmall = usageGroupInsertSQL("usage_batches", "node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at", 10, 8)
var knownUsageInsertLarge = usageGroupInsertSQL("usage_batches", "node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at", 10, 32)
var knownReceiptInsertSmall = usageGroupInsertSQL("usage_backend_receipts", "node_id,batch_id,kind,payload_sha256,confirmed_at", 5, 8)
var knownReceiptInsertLarge = usageGroupInsertSQL("usage_backend_receipts", "node_id,batch_id,kind,payload_sha256,confirmed_at", 5, 32)

func usageGroupInsertSQL(table, columns string, width, slots int) string {
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", width), ",") + ")"
	return "WITH incoming(" + columns + ") AS (VALUES " + strings.TrimSuffix(strings.Repeat(row+",", slots), ",") + ") INSERT INTO " + table + "(" + columns + ") SELECT " + columns + " FROM incoming WHERE node_id IS NOT NULL"
}

// PendingKnownUsageGroup checks a bounded snapshot of existing receipts. A
// missing receipt remains pending; only a matching durable digest suppresses
// redelivery. Confirmation revalidates the payload in its own FULL transaction.
func (s *Store) PendingKnownUsageGroup(ctx context.Context, batches []HubUsage) ([]HubUsage, error) {
	if s.owner != "hub" || len(batches) == 0 || len(batches) > 32 {
		return nil, ErrUsageBatchConflict
	}
	if len(batches) == 1 {
		if batches[0].Kind != "known" {
			return nil, ErrUsageBatchConflict
		}
		confirmed, err := s.BackendUsageConfirmed(ctx, batches[0])
		if err != nil || confirmed {
			return nil, err
		}
		return batches, nil
	}
	lookupQuery, lookupArgs := bindUsageIdentities(usageGroupConfirmedQueries, batches)
	rows, err := s.runtimeStatements[lookupQuery].QueryContext(ctx, lookupArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type receipt struct {
		priority int
		kind     string
		digest   []byte
	}
	confirmed := map[[2]string]receipt{}
	for rows.Next() {
		var node, batch string
		var r receipt
		if err := rows.Scan(&r.priority, &node, &batch, &r.kind, &r.digest); err != nil {
			return nil, err
		}
		key := [2]string{node, batch}
		if old, ok := confirmed[key]; !ok || r.priority < old.priority {
			confirmed[key] = r
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var pending []HubUsage
	for _, b := range batches {
		if b.Kind != "known" {
			return nil, ErrUsageBatchConflict
		}
		if r, ok := confirmed[[2]string{b.NodeID, b.BatchID}]; ok {
			if r.kind != b.Kind || !bytes.Equal(r.digest, b.Digest) {
				return nil, ErrUsageBatchConflict
			}
		} else {
			pending = append(pending, b)
		}
	}
	return pending, nil
}

type knownUsageEvidence struct {
	priority               int
	snapshot, upper, state string
	payload, digest        []byte
	receiptKind            sql.NullString
	receiptDigest          []byte
}

func usageGroupKeys(batches []HubUsage) ([]byte, error) {
	keys := make([][2]string, 0, len(batches))
	seen := map[[2]string]bool{}
	for _, b := range batches {
		key := [2]string{b.NodeID, b.BatchID}
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	return json.Marshal(keys)
}

func readKnownUsageSet(ctx context.Context, tx storeTransaction, batches []HubUsage) (map[[2]string]knownUsageEvidence, error) {
	lookupQuery, lookupArgs := bindUsageIdentities(knownUsageSetQueries, batches)
	rows, err := tx.QueryContext(ctx, lookupQuery, lookupArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[[2]string]knownUsageEvidence{}
	for rows.Next() {
		var node, batch string
		var e knownUsageEvidence
		if err := rows.Scan(&e.priority, &node, &batch, &e.snapshot, &e.payload, &e.digest, &e.upper, &e.state, &e.receiptKind, &e.receiptDigest); err != nil {
			return nil, err
		}
		key := [2]string{node, batch}
		if prior, ok := result[key]; !ok || e.priority < prior.priority {
			result[key] = e
		}
	}
	return result, rows.Err()
}

func execUsageGroupRows(ctx context.Context, tx storeTransaction, query string, expected int, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != int64(expected) {
		return fmt.Errorf("store: incomplete durable usage group")
	}
	return nil
}

// Returns the conflicting input index so the caller can retain the original
// dispute after rolling back the complete group.
func persistKnownUsageSet(ctx context.Context, tx storeTransaction, batches []HubUsage, decoded []UsageBatch) (int, error) {
	evidence, err := readKnownUsageSet(ctx, tx, batches)
	if err != nil {
		return 0, err
	}
	var fresh []UsageBatch
	var promote [][2]string
	for i, b := range decoded {
		key := [2]string{b.NodeID, b.BatchID}
		if e, ok := evidence[key]; ok {
			if e.priority == 2 || e.snapshot != b.SnapshotID || !bytes.Equal(e.digest, b.Digest) {
				return i, ErrUsageBatchConflict
			}
			if e.priority == 0 {
				continue
			}
			upper, err := strconv.ParseUint(e.upper, 10, 64)
			if err != nil {
				return i, err
			}
			if upper != 0 || !bytes.Equal(e.payload, b.Payload) {
				return i, ErrUsageBatchConflict
			}
			if e.state != "ledgered" {
				promote = append(promote, key)
				e.state = "ledgered"
				evidence[key] = e
			}
			continue
		}
		fresh = append(fresh, b)
		evidence[key] = knownUsageEvidence{priority: 1, snapshot: b.SnapshotID, payload: b.Payload, digest: b.Digest, upper: "0", state: "ledgered"}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if len(fresh) > 0 {
		query, slots := knownUsageInsertSmall, 8
		if len(fresh) > slots {
			query, slots = knownUsageInsertLarge, 32
		}
		args := make([]any, 10*slots)
		for i, b := range fresh {
			copy(args[10*i:], []any{b.NodeID, b.BatchID, b.BootID, b.SnapshotID, b.Payload, b.Digest, "0", "ledgered", now, now})
		}
		if err := execUsageGroupRows(ctx, tx, query, len(fresh), args...); err != nil {
			return 0, err
		}
	}
	if len(promote) > 0 {
		keys, err := json.Marshal(promote)
		if err != nil {
			return 0, err
		}
		if err := execUsageGroupRows(ctx, tx, usageSetLedgeredQuery, len(promote), now, string(keys)); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func confirmKnownUsageSet(ctx context.Context, tx storeTransaction, batches []HubUsage) error {
	evidence, err := readKnownUsageSet(ctx, tx, batches)
	if err != nil {
		return err
	}
	var pending []HubUsage
	seen := map[[2]string]bool{}
	for _, b := range batches {
		digest := sha256.Sum256(b.Payload)
		if !bytes.Equal(digest[:], b.Digest) {
			return ErrUsageBatchConflict
		}
		key := [2]string{b.NodeID, b.BatchID}
		e, ok := evidence[key]
		if !ok {
			return sql.ErrNoRows
		}
		if e.priority == 2 || !bytes.Equal(e.digest, digest[:]) {
			return ErrUsageBatchConflict
		}
		if e.priority == 0 {
			continue
		}
		if !bytes.Equal(e.payload, b.Payload) {
			return ErrUsageBatchConflict
		}
		if e.receiptKind.Valid {
			if e.receiptKind.String != "known" || !bytes.Equal(e.receiptDigest, digest[:]) {
				return ErrUsageBatchConflict
			}
			continue
		}
		if !seen[key] {
			pending = append(pending, b)
			seen[key] = true
		}
	}
	if len(pending) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	query, slots := knownReceiptInsertSmall, 8
	if len(pending) > slots {
		query, slots = knownReceiptInsertLarge, 32
	}
	args := make([]any, 5*slots)
	for i, b := range pending {
		copy(args[5*i:], []any{b.NodeID, b.BatchID, "known", b.Digest, now})
	}
	return execUsageGroupRows(ctx, tx, query, len(pending), args...)
}
