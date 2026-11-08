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
-- name: GetHubCreditGrantsByAllocation :many
SELECT g.grant_id,g.raw_granted FROM hub_credit_grants g WHERE g.tenant_id=$1 AND g.allocation_id=$2 ORDER BY g.grant_id;
-- name: GetNodeRecoveryCreditFence :one
SELECT * FROM node_recovery_credit_fences WHERE tenant_id=$1 AND id=$2;
-- name: GetNodeRecoveryCreditFenceByAllocation :one
SELECT * FROM node_recovery_credit_fences WHERE tenant_id=$1 AND recovery_id=$2 AND allocation_id=$3;
-- name: AppendNodeRecoveryCreditFence :exec
INSERT INTO node_recovery_credit_fences(tenant_id,id,recovery_id,allocation_id,node_id,boot_id,backend_id,operation_id,created_by,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: PendingNodeRecoveryCreditFences :many
SELECT f.* FROM node_recovery_credit_fences f JOIN nodes n ON n.id=f.node_id AND n.boot_id=f.boot_id
WHERE NOT EXISTS(SELECT 1 FROM node_recovery_credit_fence_receipts r WHERE r.tenant_id=f.tenant_id AND r.fence_id=f.id)
ORDER BY f.tenant_id,f.id LIMIT 100;
-- name: GetNodeRecoveryCreditFenceReceipt :one
SELECT * FROM node_recovery_credit_fence_receipts WHERE tenant_id=$1 AND fence_id=$2;
-- name: AppendNodeRecoveryCreditFenceReceipt :exec
INSERT INTO node_recovery_credit_fence_receipts(tenant_id,fence_id,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5);
-- name: MissingHubSameStoreIssues :one
SELECT EXISTS(SELECT 1 FROM hub_issue_receipts i WHERE i.hub_id=$1 AND i.hub_boot_id=$2
 AND NOT EXISTS(SELECT 1 FROM hub_same_store_journal j WHERE j.recovery_id=$3 AND j.sequence=i.ledger_sequence AND j.sequence<=sqlc.arg(sealed_sequence) AND j.kind='issue' AND j.record_id=i.issue_id AND j.payload=i.payload AND j.payload_digest=i.payload_digest));

-- name: HubSameStoreRecoveryByTargetBoot :one
SELECT * FROM hub_same_store_recoveries WHERE hub_id=$1 AND to_boot_id=$2 ORDER BY to_epoch DESC LIMIT 1;

-- name: GetNodeRecoverySources :one
SELECT * FROM node_recovery_source_provisions WHERE tenant_id=$1 AND id=$2;
-- name: GetNodeRecoverySourcesByAllocation :one
SELECT * FROM node_recovery_source_provisions WHERE tenant_id=$1 AND recovery_id=$2 AND allocation_id=$3;
-- name: AppendNodeRecoverySources :exec
INSERT INTO node_recovery_source_provisions(tenant_id,id,recovery_id,allocation_id,fence_id,node_id,boot_id,backend_id,operation_id,created_by,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: PendingNodeRecoverySources :many
SELECT p.* FROM node_recovery_source_provisions p JOIN nodes n ON n.id=p.node_id AND n.boot_id=p.boot_id
WHERE NOT EXISTS(SELECT 1 FROM node_recovery_source_receipts r WHERE r.tenant_id=p.tenant_id AND r.provision_id=p.id)
ORDER BY p.tenant_id,p.id LIMIT 100;
-- name: GetNodeRecoverySourceReceipt :one
SELECT * FROM node_recovery_source_receipts WHERE tenant_id=$1 AND provision_id=$2;
-- name: AppendNodeRecoverySourceReceipt :exec
INSERT INTO node_recovery_source_receipts(tenant_id,provision_id,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5);

-- name: GetHubSameStoreAdmission :one
SELECT * FROM hub_same_store_admissions WHERE recovery_id=$1;
-- name: AppendHubSameStoreAdmission :exec
INSERT INTO hub_same_store_admissions(tenant_id,recovery_id,hub_id,from_epoch,to_epoch,from_boot_id,to_boot_id,backend_id,operation_id,created_by,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);

-- name: ShareHasAdmittedRecovery :one
SELECT EXISTS(SELECT 1 FROM hub_allocation_share_bindings b
JOIN hub_funded_allocations a ON a.tenant_id=b.tenant_id AND a.allocation_id=b.allocation_id
JOIN hub_same_store_recoveries r ON r.hub_id=a.hub_id
JOIN hub_same_store_admissions s ON s.recovery_id=r.id
WHERE b.tenant_id=$1 AND b.node_id=$2 AND b.kind=$3 AND b.generation=$4
AND EXISTS(SELECT 1 FROM jsonb_array_elements(convert_from(r.payload,'UTF8')::jsonb->'allocations') link WHERE link->>'allocation_id'=a.allocation_id));
