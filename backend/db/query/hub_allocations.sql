-- name: FindHubAllocation :one
SELECT * FROM hub_funded_allocations WHERE tenant_id=$1 AND request_key=$2;
-- name: GetHubAllocation :one
SELECT * FROM hub_funded_allocations WHERE tenant_id=$1 AND allocation_id=$2;
-- name: HubAllocationByDelegation :one
SELECT * FROM hub_funded_allocations WHERE delegation_id=$1;
-- name: AppendHubAllocation :exec
INSERT INTO hub_funded_allocations(tenant_id,allocation_id,delegation_id,reservation_id,hub_id,hub_boot_id,node_id,boot_id,request_key,request_digest,payload,payload_digest,operation_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14);
-- name: GetHubIssue :one
SELECT * FROM hub_issue_receipts WHERE tenant_id=$1 AND issue_id=$2;
-- name: AppendHubIssue :exec
INSERT INTO hub_issue_receipts(tenant_id,issue_id,allocation_id,hub_id,ledger_sequence,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: HubAllocationIssued :one
SELECT coalesce(sum(raw_granted),0)::bigint AS issued FROM hub_credit_grants WHERE tenant_id=$1 AND allocation_id=$2;
-- name: AppendHubCreditGrant :exec
INSERT INTO hub_credit_grants(tenant_id,grant_id,allocation_id,issue_id,raw_granted) VALUES($1,$2,$3,$4,$5);
-- name: HubCreditGrant :one
SELECT g.*,a.reservation_id,a.node_id,a.boot_id FROM hub_credit_grants g JOIN hub_funded_allocations a ON a.tenant_id=g.tenant_id AND a.allocation_id=g.allocation_id WHERE g.tenant_id=$1 AND g.grant_id=$2;
-- name: HubCreditUsage :one
SELECT raw_cumulative FROM hub_credit_usage WHERE tenant_id=$1 AND grant_id=$2;
-- name: SetHubCreditUsage :exec
INSERT INTO hub_credit_usage(tenant_id,grant_id,raw_cumulative) VALUES($1,$2,$3) ON CONFLICT(tenant_id,grant_id) DO UPDATE SET raw_cumulative=excluded.raw_cumulative;
-- name: ReservationHasHubAllocation :one
SELECT EXISTS(SELECT 1 FROM hub_funded_allocations WHERE tenant_id=$1 AND reservation_id=$2);

-- name: HubSignerBoundary :one
SELECT tenant_id,signer_epoch,autonomy_until FROM signer_authorities WHERE signer_id=$1 ORDER BY signer_epoch DESC,autonomy_until,tenant_id LIMIT 1;
-- name: LatestHubIncarnation :one
SELECT * FROM hub_signer_incarnations WHERE hub_id=$1 ORDER BY signer_epoch DESC LIMIT 1;
-- name: GetHubIncarnation :one
SELECT * FROM hub_signer_incarnations WHERE hub_id=$1 AND signer_epoch=$2;
-- name: BindHubIncarnation :exec
INSERT INTO hub_signer_incarnations(hub_id,signer_epoch,boot_id) VALUES($1,$2,$3);
-- name: PendingHubAllocations :many
SELECT a.* FROM hub_funded_allocations a WHERE a.hub_id=$1 AND a.hub_boot_id=$2 AND NOT EXISTS(SELECT 1 FROM hub_allocation_receipts r WHERE r.tenant_id=a.tenant_id AND r.allocation_id=a.allocation_id) AND NOT EXISTS(SELECT 1 FROM share_renewal_closure_commands c WHERE c.tenant_id=a.tenant_id AND c.signer_id=a.hub_id AND c.hub_boot_id=a.hub_boot_id AND jsonb_exists(convert_from(c.payload,'UTF8')::jsonb->'delegation_ids',a.delegation_id)) ORDER BY a.allocation_id LIMIT 1;
-- name: GetHubAllocationGlobal :one
SELECT * FROM hub_funded_allocations WHERE allocation_id=$1;
-- name: GetHubAllocationReceipt :one
SELECT * FROM hub_allocation_receipts WHERE tenant_id=$1 AND allocation_id=$2;
-- name: AppendHubAllocationReceipt :exec
INSERT INTO hub_allocation_receipts(tenant_id,allocation_id,hub_id,hub_boot_id,payload_digest) VALUES($1,$2,$3,$4,$5);
-- name: HubBootHasInstalledAllocation :one
SELECT EXISTS(SELECT 1 FROM hub_allocation_receipts WHERE hub_id=$1 AND hub_boot_id=$2);
-- name: RuntimeAllocationExists :one
SELECT EXISTS(SELECT 1 FROM hub_funded_allocations
WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3
AND convert_from(payload,'UTF8')::jsonb->>'billing_snapshot_id'=sqlc.arg(snapshot_id)::text
AND convert_from(payload,'UTF8')::jsonb->>'signer_epoch'=sqlc.arg(signer_epoch)::text
AND convert_from(payload,'UTF8')::jsonb->>'path_generation'=sqlc.arg(path_generation)::text
AND convert_from(payload,'UTF8')::jsonb->>'fencing_generation_floor'=sqlc.arg(fencing_floor)::text);
-- name: NodeSourceCandidates :many
SELECT a.* FROM hub_funded_allocations a WHERE NOT EXISTS(SELECT 1 FROM node_source_receipts r WHERE r.tenant_id=a.tenant_id AND r.allocation_id=a.allocation_id) ORDER BY a.allocation_id LIMIT 100;
-- name: GetNodeSourceProvision :one
SELECT * FROM node_source_provisions WHERE tenant_id=$1 AND allocation_id=$2;
-- name: AppendNodeSourceProvision :exec
INSERT INTO node_source_provisions(tenant_id,allocation_id,node_id,boot_id,backend_id,operation_id,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: GetNodeSourceReceipt :one
SELECT * FROM node_source_receipts WHERE tenant_id=$1 AND allocation_id=$2;
-- name: AppendNodeSourceReceipt :exec
INSERT INTO node_source_receipts(tenant_id,allocation_id,payload,payload_digest) VALUES($1,$2,$3,$4);

-- name: HubAllocationIssueInventory :many
SELECT i.issue_id,i.payload_digest,g.raw_granted FROM hub_issue_receipts i JOIN hub_credit_grants g ON g.tenant_id=i.tenant_id AND g.issue_id=i.issue_id WHERE i.tenant_id=$1 AND i.allocation_id=$2 ORDER BY i.issue_id;
-- name: GetHubDelegationSeal :one
SELECT * FROM hub_delegation_seals WHERE tenant_id=$1 AND allocation_id=$2;
-- name: AppendHubDelegationSeal :exec
INSERT INTO hub_delegation_seals(tenant_id,allocation_id,seal_id,hub_id,hub_boot_id,ledger_sequence,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
