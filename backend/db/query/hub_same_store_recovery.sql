-- name: GetHubSameStoreRecovery :one
SELECT * FROM hub_same_store_recoveries WHERE hub_id=$1 AND to_epoch=$2;
-- name: AppendHubSameStoreRecovery :exec
INSERT INTO hub_same_store_recoveries(tenant_id,id,hub_id,from_epoch,to_epoch,from_boot_id,to_boot_id,backend_id,operation_id,created_by,payload,payload_digest,proof,proof_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14);

-- name: HubSameStoreRecoveryByID :one
SELECT * FROM hub_same_store_recoveries WHERE id=$1;
-- name: GetHubSameStoreJournal :one
SELECT * FROM hub_same_store_journal WHERE recovery_id=$1 AND sequence=$2;
-- name: AppendHubSameStoreJournal :exec
INSERT INTO hub_same_store_journal(tenant_id,recovery_id,sequence,kind,record_id,payload,payload_digest,peer_fingerprint,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: PageHubSameStoreJournal :many
SELECT * FROM hub_same_store_journal WHERE recovery_id=$1 AND sequence>sqlc.arg(after_sequence) ORDER BY sequence LIMIT 32;
-- name: GetHubSameStoreManifest :one
SELECT * FROM hub_same_store_manifests WHERE recovery_id=$1;
-- name: AppendHubSameStoreManifest :exec
INSERT INTO hub_same_store_manifests(tenant_id,recovery_id,sealed_sequence,payload,payload_digest,peer_fingerprint,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: MissingHubSameStoreIssues :one
SELECT EXISTS(SELECT 1 FROM hub_issue_receipts i WHERE i.hub_id=$1 AND i.hub_boot_id=$2
 AND NOT EXISTS(SELECT 1 FROM hub_same_store_journal j WHERE j.recovery_id=$3 AND j.sequence=i.ledger_sequence AND j.sequence<=sqlc.arg(sealed_sequence) AND j.kind='issue' AND j.record_id=i.issue_id AND j.payload=i.payload AND j.payload_digest=i.payload_digest));
