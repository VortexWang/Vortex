-- name: FindHubAllocation :one
SELECT * FROM hub_funded_allocations WHERE tenant_id=$1 AND request_key=$2;
-- name: GetHubAllocation :one
SELECT * FROM hub_funded_allocations WHERE tenant_id=$1 AND allocation_id=$2;
-- name: HubAllocationByDelegation :one
SELECT * FROM hub_funded_allocations WHERE delegation_id=$1;
-- name: AppendHubAllocation :exec
INSERT INTO hub_funded_allocations(tenant_id,allocation_id,delegation_id,reservation_id,hub_id,node_id,boot_id,request_key,request_digest,payload,payload_digest,operation_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);
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
