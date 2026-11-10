-- name: GetHubColdRecovery :one
SELECT * FROM hub_cold_recoveries WHERE hub_id=$1 AND to_epoch=$2;
-- name: AppendHubColdRecovery :exec
INSERT INTO hub_cold_recoveries(tenant_id,id,hub_id,from_epoch,to_epoch,from_boot_id,to_boot_id,payload,payload_digest,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);
-- name: HubRecoveryAuthorities :many
SELECT * FROM signer_authorities WHERE signer_id=$1 AND signer_epoch<=sqlc.arg(to_epoch) ORDER BY tenant_id,signer_epoch;
-- name: HubRecoveryAllocations :many
SELECT * FROM hub_funded_allocations WHERE hub_id=$1 ORDER BY tenant_id,allocation_id;
-- name: HubRecoveryShares :many
SELECT * FROM share_generations WHERE signer_id=$1 ORDER BY tenant_id,node_id,kind,generation;
