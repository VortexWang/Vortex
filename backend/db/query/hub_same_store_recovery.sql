-- name: GetHubSameStoreRecovery :one
SELECT * FROM hub_same_store_recoveries WHERE hub_id=$1 AND to_epoch=$2;
-- name: AppendHubSameStoreRecovery :exec
INSERT INTO hub_same_store_recoveries(tenant_id,id,hub_id,from_epoch,to_epoch,from_boot_id,to_boot_id,backend_id,operation_id,created_by,payload,payload_digest,proof,proof_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14);
