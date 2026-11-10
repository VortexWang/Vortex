package agent

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestProcessFundingAuditRequiresExactDurablePendingIssue(t *testing.T) {
	for _, variant := range []string{"lost-response", "installed", "missing-request", "changed-request", "changed-scope", "partial-install", "changed-digest", "changed-installed-grant"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			agentPath, hubPath := filepath.Join(dir, "agent.db"), filepath.Join(dir, "hub.db")
			as, err := store.Open(agentPath, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer as.Close()
			hs, err := store.Open(hubPath, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			n, err := New(controlTestNodeID, as, nil)
			if err != nil {
				t.Fatal(err)
			}
			id := func(i int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i) }
			now := time.Now().UTC()
			d := store.HubDelegation{ID: id(201), HubID: id(202), SourceID: id(203), RuntimeSourceID: id(204), ShareSourceID: id(205), NodeID: n.NodeID, BootID: n.BootID, BillingSnapshotID: id(206), RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, FencingFloor: 1, RawBytes: 1000, RateBPS: 1000000, Connections: 1, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
			clock := store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
			if err := hs.InstallHubDelegation(ctx, d, clock); err != nil {
				t.Fatal(err)
			}
			scope := store.DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
			request, err := as.PrepareDemand(ctx, scope, 100, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			issued, err := hs.IssueHubAuthorization(ctx, request, clock)
			if err != nil {
				t.Fatal(err)
			}
			a, err := sql.Open("sqlite", agentPath)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			h, err := sql.Open("sqlite", hubPath)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			if variant == "installed" || variant == "partial-install" || variant == "changed-installed-grant" {
				if err := (&ControlClient{Node: n}).applyAuthorizationUpdateFrom(ctx, &controlv1.AuthorizationUpdate{Generation: 1, Payload: issued.Payload}, d.HubID); err != nil {
					t.Fatal(err)
				}
			}
			switch variant {
			case "missing-request":
				_, err = a.Exec(`DELETE FROM authorization_demands`)
			case "changed-request":
				_, err = a.Exec(`UPDATE authorization_demands SET request='{}'`)
			case "changed-scope":
				_, err = a.Exec(`UPDATE authorization_demands SET scope='{}'`)
			case "partial-install":
				_, err = a.Exec(`DELETE FROM authorization_updates`)
			case "changed-digest":
				_, err = h.Exec(`UPDATE issuer_issues SET payload_sha256=X'00'`)
			case "changed-installed-grant":
				_, err = a.Exec(`UPDATE authorization_records SET amount_bytes='99' WHERE kind='credit_grant'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := auditProcessFunding(ctx, a, h, d)
			if variant != "lost-response" && variant != "installed" {
				if err == nil {
					t.Fatal("invalid outstanding funding accepted", inventory)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if variant == "lost-response" && (inventory.Installed != 0 || inventory.Uninstalled != 100 || len(inventory.PendingIssues) != 1) {
				t.Fatal("lost issue not retained", inventory)
			}
			if variant == "installed" && (inventory.Installed != 100 || inventory.Uninstalled != 0 || len(inventory.PendingIssues) != 0) {
				t.Fatal("installed issue double-counted", inventory)
			}
		})
	}
}
