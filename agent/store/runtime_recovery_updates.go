package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
)

// RuntimeRecoveryUpdates scans at most 32 path scopes for the authenticated Node
// incarnation after a completed finite handoff. The cursor observes no rights;
// immutable FULL outbox payloads are replayed before any later runtime grants.
// Engine's most recent rule scope cannot hide other installed paths.
func (s *Store) RuntimeRecoveryUpdates(ctx context.Context, hub, node, boot, after string, now IssuerTime) (BootstrapPage, error) {
	var page BootstrapPage
	if s.owner != "hub" || !validIssuerUUID(hub) || !validIssuerUUID(node) || !validIssuerUUID(boot) {
		return page, ErrAuthorizationDenied
	}
	if e := checkIssuerTime(now); e != nil {
		return page, e
	}
	var cursor uint64
	if after != "" {
		v, e := strconv.ParseUint(after, 10, 63)
		if e != nil || v == 0 || strconv.FormatUint(v, 10) != after {
			return page, ErrAuthorizationDenied
		}
		cursor = v
	}
	snapshot, e := s.HubControlSnapshot(ctx, hub, now.Latest)
	if e != nil {
		return page, e
	}
	if snapshot.Mode != "online" {
		page.Complete = true
		return page, nil
	}
	var handoff string
	e = s.db.QueryRowContext(ctx, `SELECT h.handoff_id FROM hub_handoffs h JOIN issuer_epochs i ON i.hub_id=h.hub_id AND i.signer_epoch=h.to_epoch JOIN hub_recoveries r ON r.handoff_id=h.handoff_id AND r.completed=1 WHERE h.hub_id=? ORDER BY CAST(h.to_epoch AS INTEGER) DESC LIMIT 1`, hub).Scan(&handoff)
	if errors.Is(e, sql.ErrNoRows) {
		page.Complete = true
		return page, nil
	}
	if e != nil {
		return page, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT DISTINCT CAST(json_extract(CAST(d.payload AS TEXT),'$.PathGeneration') AS INTEGER) FROM issuer_delegations d WHERE d.hub_id=? AND json_extract(CAST(d.payload AS TEXT),'$.NodeID')=? AND json_extract(CAST(d.payload AS TEXT),'$.BootID')=? AND CAST(json_extract(CAST(d.payload AS TEXT),'$.PathGeneration') AS INTEGER)>? AND EXISTS(SELECT 1 FROM issuer_issues i WHERE i.delegation_id=d.delegation_id) AND (d.state='revoked' OR EXISTS(SELECT 1 FROM hub_transfers t WHERE t.handoff_id=? AND t.delegation_id=d.delegation_id)) ORDER BY 1 LIMIT 32`, hub, node, boot, cursor, handoff)
	if e != nil {
		return page, e
	}
	var paths []uint64
	for rows.Next() {
		var path uint64
		if e = rows.Scan(&path); e != nil {
			rows.Close()
			return page, e
		}
		if path == 0 || path > math.MaxInt64 {
			rows.Close()
			return page, ErrAuthorizationDenied
		}
		paths = append(paths, path)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return page, e
	}
	page.Complete = len(paths) < 32
	for _, path := range paths {
		page.After = strconv.FormatUint(path, 10)
		update, e := s.RecoveryAuthorization(ctx, handoff, node, boot, path, now)
		if e != nil {
			return BootstrapPage{}, e
		}
		page.Updates = append(page.Updates, update)
	}
	return page, nil
}
