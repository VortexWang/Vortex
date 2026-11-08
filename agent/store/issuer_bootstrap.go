package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// BootstrapPage is a bounded scan of backend-funded delegations for one
// authenticated Node incarnation. The cursor changes no authority or balance.
type BootstrapPage struct {
	Updates  []IssuedAuthorization
	After    string
	Complete bool
}

// BootstrapHubAuthorizations creates one deterministic initial issue per finite
// delegation. Reconnects and competing streams replay the same FULL journal
// bytes, never another partition. Subsequent top-ups use explicit renewal or a
// new source-funded allocation. A Node cannot choose the budget, rule or epoch.
func (s *Store) BootstrapHubAuthorizations(ctx context.Context, hub, node, boot, after string, now IssuerTime) (BootstrapPage, error) {
	var page BootstrapPage
	if s.owner != "hub" || !validIssuerUUID(hub) || !validIssuerUUID(node) || !validIssuerUUID(boot) || after != "" && !validIssuerUUID(after) {
		return page, ErrAuthorizationDenied
	}
	if e := checkIssuerTime(now); e != nil {
		return page, e
	}
	if now.Latest.Sub(now.Earliest) > 10*time.Second {
		return page, ErrAuthorityTimeUncertain
	}
	rows, e := s.db.QueryContext(ctx, `SELECT payload,payload_sha256 FROM issuer_delegations WHERE hub_id=? AND state='open' AND delegation_id>? AND json_extract(CAST(payload AS TEXT),'$.NodeID')=? AND json_extract(CAST(payload AS TEXT),'$.BootID')=? ORDER BY delegation_id LIMIT 32`, hub, after, node, boot)
	if e != nil {
		return page, e
	}
	var delegations []HubDelegation
	for rows.Next() {
		var raw, digest []byte
		if e = rows.Scan(&raw, &digest); e != nil {
			rows.Close()
			return page, e
		}
		sum := sha256.Sum256(raw)
		var d HubDelegation
		if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &d) != nil || d.HubID != hub || d.NodeID != node || d.BootID != boot {
			rows.Close()
			return page, ErrAuthorizationDenied
		}
		delegations = append(delegations, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return page, e
	}
	page.Complete = len(delegations) < 32
	for _, d := range delegations {
		page.After = d.ID
		// Only this candidate's explicitly provisioned finite three-root shape
		// participates; historical test/legacy delegations cannot auto-activate.
		if len(d.ShareReferences) != 3 || now.Earliest.Before(d.ValidFrom) || !now.Latest.Before(d.ExpiresAt) || !now.Latest.Before(d.HardDeadline) || !now.Latest.Before(d.AutonomyDeadline) {
			continue
		}
		sum := sha256.Sum256([]byte("vortex/funded-bootstrap/v1/" + d.ID))
		id := sum[:16]
		id[6] = id[6]&0x0f | 0x50
		id[8] = id[8]&0x3f | 0x80
		requestID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
		issue, e := s.IssueHubAuthorization(ctx, IssueRequest{ID: requestID, HubID: hub, DelegationID: d.ID, NodeID: node, BootID: boot, RawBytes: min(d.RawBytes, 1<<20), TTL: 300 * time.Second}, now)
		if e != nil {
			return BootstrapPage{}, e
		}
		page.Updates = append(page.Updates, issue)
	}
	return page, nil
}
