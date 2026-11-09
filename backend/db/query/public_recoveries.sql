-- Safe business stages. A recorded retirement/admission is not a claim of
-- local readiness, Engine activation or financial release.
-- name: GetPublicNodeRecovery :one
SELECT jsonb_build_object('id',r.id,'resource_id',r.node_id,'mode','node_boot',
 'operation_id',r.operation_id,'stage',CASE WHEN EXISTS(SELECT 1 FROM node_boot_recovery_receipts p WHERE p.tenant_id=r.tenant_id AND p.recovery_id=r.id) THEN 'retirement_recorded' ELSE 'awaiting_retirement_receipt' END) AS body
FROM node_boot_recoveries r WHERE r.tenant_id=$1 AND r.id=$2;

-- name: GetPublicHubSameStoreRecovery :one
SELECT jsonb_build_object('id',r.id,'resource_id',r.hub_id,'mode','hub_same_store',
 'operation_id',r.operation_id,'stage',CASE
 WHEN EXISTS(SELECT 1 FROM hub_same_store_admissions a WHERE a.tenant_id=r.tenant_id AND a.recovery_id=r.id) THEN 'admission_recorded'
 WHEN EXISTS(SELECT 1 FROM hub_same_store_manifests m WHERE m.recovery_id=r.id) THEN 'manifest_recorded'
 ELSE 'awaiting_manifest' END) AS body
FROM hub_same_store_recoveries r WHERE r.tenant_id=$1 AND r.id=$2;

-- name: GetPublicHubColdRecovery :one
SELECT jsonb_build_object('id',r.id,'resource_id',r.hub_id,'mode','hub_cold','stage','expiry_approved') AS body
FROM hub_cold_recoveries r WHERE r.tenant_id=$1 AND r.id=$2;

-- name: ScopedHubRecoveryContext :one
SELECT backend_id,operation_id FROM hub_same_store_recoveries WHERE tenant_id=$1 AND id=$2;
