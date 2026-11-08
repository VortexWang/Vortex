-- name: EnsureShareChain :exec
INSERT INTO share_chains(tenant_id,node_id,kind,boot_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: LockShareChain :one
SELECT * FROM share_chains WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 FOR UPDATE;
-- name: ShareGenerationByRequest :one
SELECT * FROM share_generations WHERE tenant_id=$1 AND request_key=$2;
-- name: GetShareGeneration :one
SELECT * FROM share_generations WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4;
-- name: AppendShareGeneration :one
INSERT INTO share_generations(tenant_id,node_id,kind,generation,predecessor,id,boot_id,signer_id,signer_epoch,path_generation,fencing_generation,amount,burst_bytes,billing_snapshot_id,rule_id,rule_revision,created_at,ownership_until,policy_until,autonomy_until,request_key,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) RETURNING *;
-- name: ReserveShareSuccessor :execrows
UPDATE share_chains SET head_generation=$4,pending_generation=$4,held_amount=GREATEST(held_amount,$5) WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND head_generation=$6 AND pending_generation=0;
-- name: TotalShareHeld :one
SELECT COALESCE(sum(held_amount),0)::bigint FROM share_chains WHERE tenant_id=$1 AND kind=$2;
-- name: AppendShareRenewalAuthority :exec
INSERT INTO share_renewal_authorities(tenant_id,node_id,kind,generation,signer_id,signer_epoch) VALUES($1,$2,$3,$4,$5,$6);
-- name: ShareRenewalAuthority :one
SELECT * FROM share_renewal_authorities WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4 AND signer_id=$5;
-- name: CloseShareRenewalAuthority :execrows
UPDATE share_renewal_authorities SET closed_at=$6,closure_proof=$7 WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4 AND signer_id=$5 AND closed_at IS NULL;
-- name: OpenShareRenewalCount :one
SELECT count(*) FROM share_renewal_authorities WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4 AND closed_at IS NULL;
-- name: CreateShareHandoff :one
INSERT INTO share_handoffs(tenant_id,id,node_id,kind,generation,operation_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetShareHandoff :one
SELECT * FROM share_handoffs WHERE tenant_id=$1 AND id=$2;
-- name: ShareHandoffByGeneration :one
SELECT * FROM share_handoffs WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4;
-- name: EnsureNodeFencing :exec
INSERT INTO node_fencing(tenant_id,node_id,boot_id,fencing_generation) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: LockNodeFencing :one
SELECT * FROM node_fencing WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3 FOR UPDATE;
-- name: NodeOtherBootHeld :one
SELECT EXISTS(SELECT 1 FROM share_chains WHERE tenant_id=$1 AND node_id=$2 AND boot_id<>$3 AND held_amount>0);
-- name: RaiseNodeFencing :execrows
UPDATE node_fencing SET fencing_generation=$4 WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3 AND fencing_generation<=$4 AND NOT renewal_closed;

-- name: AppendSignerAuthority :one
INSERT INTO signer_authorities(tenant_id,signer_id,signer_epoch,heartbeat_at,autonomy_until,approval_evidence) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetSignerAuthority :one
SELECT * FROM signer_authorities WHERE tenant_id=$1 AND signer_id=$2 AND signer_epoch=$3;
-- name: LatestSignerEpoch :one
SELECT COALESCE(max(signer_epoch),0)::bigint FROM signer_authorities WHERE tenant_id=$1 AND signer_id=$2;

-- name: ReadShareChain :one
SELECT * FROM share_chains WHERE tenant_id=$1 AND node_id=$2 AND kind=$3;
-- name: GetNodeIncarnation :one
SELECT boot_id,generation FROM nodes WHERE id=$1;
