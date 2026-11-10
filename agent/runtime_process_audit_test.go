package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"vortex/agent/store"
)

type processFundingInventory struct {
	Installed, Uninstalled uint64
	PendingIssues          []string
}

// Shutdown can lose the last response after Hub FULL issuance. Such credit
// must remain funded and bound to the original pending request, rather than
// being mistaken for installed Agent credit or refunded by this audit.
func auditProcessFunding(ctx context.Context, agent, hub *sql.DB, d store.HubDelegation) (processFundingInventory, error) {
	var result processFundingInventory
	rows, err := hub.QueryContext(ctx, `SELECT issue_id,request,payload,payload_sha256,amount_bytes FROM issuer_issues WHERE delegation_id=?`, d.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, amountText string
		var requestRaw, payload, digest []byte
		if err := rows.Scan(&id, &requestRaw, &payload, &digest, &amountText); err != nil {
			return result, err
		}
		var request store.IssueRequest
		amount, err := strconv.ParseUint(amountText, 10, 64)
		if err != nil || json.Unmarshal(requestRaw, &request) != nil || request.ID != id || request.NodeID != d.NodeID || request.HubID != d.HubID || request.DelegationID != d.ID || request.RawBytes != amount || amount == 0 || amount > d.RawBytes {
			return result, fmt.Errorf("invalid durable issue %s", id)
		}
		if d.BootID == "" {
			d.BootID = request.BootID
		}
		if request.BootID != d.BootID {
			return result, fmt.Errorf("issue boot changed %s", id)
		}
		hash := sha256.Sum256(payload)
		p, err := validateAuthorizationUpdate(payload, d.NodeID, d.BootID, d.PathGeneration)
		if err != nil || !bytes.Equal(hash[:], digest) || p.UpdateID != id || p.SignerEpoch != d.SignerEpoch || p.FencingGenerationFloor != d.FencingFloor || len(p.Authorizations) != 3 {
			return result, fmt.Errorf("invalid issued payload %s: %v", id, err)
		}
		var grant authorizationRecord
		kinds := map[string]bool{}
		for _, record := range p.Authorizations {
			if kinds[record.Kind] || record.ParentAuthorizationID != d.ID || record.RuleID != d.RuleID || record.RuleRevision != d.RuleRevision || record.HardDeadline.After(d.HardDeadline) {
				return result, fmt.Errorf("issued scope changed %s", id)
			}
			kinds[record.Kind] = true
			if record.Kind == "credit_grant" {
				grant = record
				if record.SourceID != d.SourceID || record.BillingSnapshotID != d.BillingSnapshotID || record.AmountBytes == nil || *record.AmountBytes != amount {
					return result, fmt.Errorf("issued funding changed %s", id)
				}
			}
		}
		if grant.AuthorizationID == "" || !kinds["runtime_lease"] || !kinds["share_lease"] {
			return result, fmt.Errorf("incomplete issue %s", id)
		}
		var journalPayload, journalDigest []byte
		if err := hub.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM hub_ledger WHERE kind='issue' AND record_id=?`, id).Scan(&journalPayload, &journalDigest); err != nil || !bytes.Equal(payload, journalPayload) || !bytes.Equal(digest, journalDigest) {
			return result, fmt.Errorf("issue journal mismatch %s: %v", id, err)
		}
		var installedPayload, installedDigest []byte
		var issuer, boot string
		err = agent.QueryRowContext(ctx, `SELECT payload,payload_sha256,issuer_id,boot_id FROM authorization_updates WHERE node_id=? AND update_id=?`, d.NodeID, id).Scan(&installedPayload, &installedDigest, &issuer, &boot)
		if err == nil {
			if !bytes.Equal(payload, installedPayload) || !bytes.Equal(digest, installedDigest) || issuer != d.HubID || boot != d.BootID {
				return result, fmt.Errorf("installed payload mismatch %s", id)
			}
			var raw, source, parent string
			if err := agent.QueryRowContext(ctx, `SELECT amount_bytes,source_id,parent_authorization_id FROM authorization_records WHERE node_id=? AND authorization_id=? AND kind='credit_grant'`, d.NodeID, grant.AuthorizationID).Scan(&raw, &source, &parent); err != nil || raw != amountText || source != d.SourceID || parent != d.ID {
				return result, fmt.Errorf("installed grant mismatch %s: %v", id, err)
			}
			if result.Installed > d.RawBytes-amount {
				return result, fmt.Errorf("installed funds exceed delegation")
			}
			result.Installed += amount
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		var pendingRequest, scopeRaw []byte
		var state string
		if err := agent.QueryRowContext(ctx, `SELECT request,scope,state FROM authorization_demands WHERE request_id=?`, id).Scan(&pendingRequest, &scopeRaw, &state); err != nil || state != "pending" || !bytes.Equal(pendingRequest, requestRaw) {
			return result, fmt.Errorf("uninstalled issue has no exact pending request %s: %v", id, err)
		}
		var scope store.DemandScope
		expected := store.DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: d.SignerEpoch, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration}
		if json.Unmarshal(scopeRaw, &scope) != nil || scope != expected {
			return result, fmt.Errorf("pending issue scope changed %s", id)
		}
		for _, record := range p.Authorizations {
			var present bool
			if err := agent.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM authorization_records WHERE node_id=? AND authorization_id=?)`, d.NodeID, record.AuthorizationID).Scan(&present); err != nil || present {
				return result, fmt.Errorf("partial issue installation %s: %v", id, err)
			}
		}
		if result.Uninstalled > d.RawBytes-amount {
			return result, fmt.Errorf("pending funds exceed delegation")
		}
		result.Uninstalled += amount
		result.PendingIssues = append(result.PendingIssues, id)
	}
	return result, rows.Err()
}
