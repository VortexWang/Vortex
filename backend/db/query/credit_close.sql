-- name: CreditCloseCandidates :many
SELECT g.*,a.created_by,a.node_id,a.boot_id FROM hub_credit_grants g
JOIN hub_funded_allocations a ON a.tenant_id=g.tenant_id AND a.allocation_id=g.allocation_id
JOIN hub_delegation_seals s ON s.tenant_id=g.tenant_id AND s.allocation_id=g.allocation_id
WHERE NOT EXISTS(SELECT 1 FROM node_credit_close_receipts r WHERE r.tenant_id=g.tenant_id AND r.grant_id=g.grant_id)
ORDER BY g.tenant_id,g.grant_id LIMIT 100;

-- name: GetCreditCloseCommand :one
SELECT * FROM node_credit_close_commands WHERE tenant_id=$1 AND grant_id=$2;

-- name: AppendCreditCloseCommand :exec
INSERT INTO node_credit_close_commands(tenant_id,grant_id,allocation_id,command_id,node_id,boot_id,backend_id,operation_id,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: GetCreditCloseReceipt :one
SELECT * FROM node_credit_close_receipts WHERE tenant_id=$1 AND grant_id=$2;

-- name: AppendCreditCloseReceipt :exec
INSERT INTO node_credit_close_receipts(tenant_id,grant_id,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5);

-- name: ChildKnownUsage :many
SELECT * FROM usage_batches WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3
AND jsonb_path_exists(facts,'$[*] ? (@.grant_id == $grant)',jsonb_build_object('grant',sqlc.arg(grant_id)::text))
ORDER BY batch_id;

-- name: ReservationDisputed :one
SELECT EXISTS(SELECT 1 FROM disputes WHERE tenant_id=$1 AND grant_id=$2);

-- name: HubAllocationChildren :many
SELECT * FROM hub_credit_grants WHERE tenant_id=$1 AND allocation_id=$2 ORDER BY grant_id;

-- name: HubCreditSettlementCandidates :many
SELECT a.* FROM hub_funded_allocations a
JOIN hub_delegation_seals s ON s.tenant_id=a.tenant_id AND s.allocation_id=a.allocation_id
WHERE NOT EXISTS(SELECT 1 FROM settlements t WHERE t.tenant_id=a.tenant_id AND t.grant_id=a.reservation_id AND t.kind IN ('final','conservative'))
AND NOT EXISTS(SELECT 1 FROM hub_credit_grants g WHERE g.tenant_id=a.tenant_id AND g.allocation_id=a.allocation_id
AND NOT EXISTS(SELECT 1 FROM node_credit_close_receipts r WHERE r.tenant_id=g.tenant_id AND r.grant_id=g.grant_id))
ORDER BY a.tenant_id,a.allocation_id LIMIT 32;
