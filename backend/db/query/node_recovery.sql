-- name: GetNodeBootRecovery :one
SELECT * FROM node_boot_recoveries WHERE tenant_id=$1 AND id=$2;
-- name: NodeBootRecoveryByOldBoot :one
SELECT * FROM node_boot_recoveries WHERE tenant_id=$1 AND node_id=$2 AND from_boot_id=$3;
-- name: AppendNodeBootRecovery :exec
INSERT INTO node_boot_recoveries(tenant_id,id,node_id,from_boot_id,to_boot_id,backend_id,operation_id,created_by,proof,proof_digest,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: GetNodeBootRecoveryReceipt :one
SELECT * FROM node_boot_recovery_receipts WHERE tenant_id=$1 AND recovery_id=$2;
-- name: AppendNodeBootRecoveryReceipt :exec
INSERT INTO node_boot_recovery_receipts(tenant_id,recovery_id,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5);
-- name: LockNodeRecoveryIncarnation :one
SELECT boot_id,generation FROM nodes WHERE id=$1 AND deleted_at IS NULL FOR UPDATE;
-- name: NodeRecoveryShares :many
SELECT * FROM share_generations WHERE node_id=$1 AND boot_id=$2 ORDER BY tenant_id,kind,generation;
-- name: NodeRecoveryAllocations :many
SELECT * FROM hub_funded_allocations WHERE node_id=$1 AND boot_id=$2 ORDER BY tenant_id,allocation_id;
-- name: NodeRecoveryLeases :many
SELECT * FROM lease_expectations WHERE node_id=$1 AND boot_id=$2 ORDER BY tenant_id,kind,revision;
-- name: NodeRecoveryConfigurations :many
SELECT m.*,d.deadline FROM rule_deployment_members m JOIN rule_deployments d ON d.tenant_id=m.tenant_id AND d.id=m.deployment_id WHERE m.node_id=$1 AND m.boot_id=$2 ORDER BY m.tenant_id,m.deployment_id;
-- name: RetireNodeShareChain :execrows
UPDATE share_chains SET boot_id=sqlc.arg(new_boot),active_generation=0,retired_generation=head_generation
WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND boot_id=sqlc.arg(old_boot) AND head_generation=sqlc.arg(generation) AND active_generation=head_generation AND pending_generation=0 AND retired_generation=0;
-- name: CloseNodeBootRenewal :exec
UPDATE share_renewal_authorities a SET closed_at=$4,closure_proof=$5 WHERE a.tenant_id=$1 AND a.node_id=$2 AND a.closed_at IS NULL AND EXISTS(SELECT 1 FROM share_generations g WHERE g.tenant_id=a.tenant_id AND g.node_id=a.node_id AND g.kind=a.kind AND g.generation=a.generation AND g.boot_id=$3);
-- name: CloseNodeBootFencing :execrows
UPDATE node_fencing SET renewal_closed=true,closure_proof=$4 WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3 AND NOT renewal_closed;
-- name: LockNodeAuthorityRead :exec
SELECT pg_advisory_xact_lock_shared(hashtextextended('node-authority/' || sqlc.arg(node_id)::text, 0));
-- name: LockNodeAuthorityWrite :exec
SELECT pg_advisory_xact_lock(hashtextextended('node-authority/' || sqlc.arg(node_id)::text, 0));
