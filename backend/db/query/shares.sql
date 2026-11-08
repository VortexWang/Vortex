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
INSERT INTO share_handoffs(tenant_id,id,node_id,kind,generation,operation_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;
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

-- name: ShareClosureEvidence :many
SELECT * FROM share_renewal_authorities WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND generation=$4 ORDER BY signer_id;
-- name: BindShareCommand :execrows
UPDATE share_handoffs SET command=$3,command_digest=$4,state='cas_pending',execution_state='enforcing' WHERE tenant_id=$1 AND id=$2 AND command IS NULL AND state='prepared';
-- name: BindShareDelivery :exec
INSERT INTO share_delivery_bindings(tenant_id,task_id,handoff_id,phase) VALUES($1,$2,$3,$4);
-- name: GetShareDelivery :one
SELECT b.*,h.node_id,h.kind,h.generation,h.command,h.command_digest,h.created_by,g.boot_id FROM share_delivery_bindings b JOIN share_handoffs h ON h.tenant_id=b.tenant_id AND h.id=b.handoff_id JOIN share_generations g ON g.tenant_id=h.tenant_id AND g.node_id=h.node_id AND g.kind=h.kind AND g.generation=h.generation WHERE b.tenant_id=$1 AND b.task_id=$2;
-- name: PendingShareDelivery :one
SELECT EXISTS(SELECT 1 FROM share_delivery_bindings b JOIN tasks t ON t.tenant_id=b.tenant_id AND t.id=b.task_id WHERE b.tenant_id=$1 AND b.handoff_id=$2 AND t.state IN ('pending','leased','running'));
-- name: ShareDeliveryCandidates :many
SELECT t.*,h.created_by FROM tasks t JOIN share_delivery_bindings b ON b.tenant_id=t.tenant_id AND b.task_id=t.id JOIN share_handoffs h ON h.tenant_id=b.tenant_id AND h.id=b.handoff_id WHERE t.state='pending' AND h.next_attempt<=now() ORDER BY t.id LIMIT 100;
-- name: ExpiredShareDeliveries :many
SELECT t.*,h.created_by FROM tasks t JOIN share_delivery_bindings b ON b.tenant_id=t.tenant_id AND b.task_id=t.id JOIN share_handoffs h ON h.tenant_id=b.tenant_id AND h.id=b.handoff_id WHERE t.state IN ('leased','running') AND t.lease_until<=now() ORDER BY t.id LIMIT 100;
-- name: LeaseShareDelivery :one
UPDATE tasks SET state='running',lease_token=$3,lease_until=now()+interval '10 seconds',attempts=attempts+1 WHERE tenant_id=$1 AND id=$2 AND state='pending' RETURNING *;
-- name: ShareTaskByLease :one
SELECT * FROM tasks WHERE tenant_id=$1 AND id=$2 AND lease_token=$3 FOR UPDATE;
-- name: FinishShareTask :exec
UPDATE tasks SET state=$3,lease_until=NULL WHERE tenant_id=$1 AND id=$2;
-- name: ShareExecutionUnknown :exec
UPDATE share_handoffs SET state='activation-unknown',execution_state='enforcing',attempts=attempts+1,next_attempt=now()+interval '1 second' WHERE tenant_id=$1 AND id=$2 AND state<>'enforced';
-- name: GetShareReceipt :one
SELECT * FROM share_handoff_receipts WHERE tenant_id=$1 AND receipt_id=$2;
-- name: AppendShareReceipt :exec
INSERT INTO share_handoff_receipts(tenant_id,handoff_id,receipt_id,command_digest,receipt_digest,peer_fingerprint,payload) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: ConfirmShareChain :execrows
UPDATE share_chains SET active_generation=$4,pending_generation=0,held_amount=$5 WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND pending_generation=$4 AND active_generation IN ($4,$6);
-- name: ConfirmShareHandoff :exec
UPDATE share_handoffs SET state='enforced',execution_state='enforced',receipt_digest=$3 WHERE tenant_id=$1 AND id=$2;

-- name: ActivateDrainingShare :execrows
UPDATE share_chains SET active_generation=$4 WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND pending_generation=$4 AND active_generation IN ($4,$5);

-- name: PreparedShareArtifacts :many
SELECT a.*,m.boot_id,m.transaction_id,d.id AS deployment_id,r.created_by
FROM rule_artifacts a
JOIN rule_artifact_commits ac ON ac.tenant_id=a.tenant_id AND ac.digest=a.digest
JOIN rule_deployments d ON d.tenant_id=a.tenant_id AND d.rule_id=a.rule_id AND d.rule_revision=a.rule_revision AND d.input_digest=a.input_digest
JOIN rule_deployment_members m ON m.tenant_id=d.tenant_id AND m.deployment_id=d.id AND m.node_id=a.node_id AND m.artifact_digest=a.digest
JOIN rule_heads h ON h.tenant_id=a.tenant_id AND h.id=a.rule_id AND h.desired_revision=a.rule_revision
JOIN rule_revisions r ON r.tenant_id=a.tenant_id AND r.rule_id=a.rule_id AND r.revision=a.rule_revision
WHERE a.tenant_id=$1 AND m.state IN ('prepared','active','unknown') AND d.state NOT IN ('failed','superseded')
AND (convert_from(r.payload,'UTF8')::jsonb->>'enabled')::boolean
AND EXISTS(SELECT 1 FROM rule_delivery_bindings b JOIN rule_delivery_receipts p ON p.tenant_id=b.tenant_id AND p.task_id=b.task_id JOIN tasks t ON t.tenant_id=b.tenant_id AND t.id=b.task_id WHERE b.tenant_id=a.tenant_id AND b.deployment_id=d.id AND b.node_id=a.node_id AND b.phase='prepare' AND t.state='succeeded')
ORDER BY a.node_id,a.rule_id,a.digest;
-- name: GetShareAllocationWindow :one
SELECT * FROM share_allocation_windows WHERE tenant_id=$1 AND window_at=$2;
-- name: AppendShareAllocationWindow :exec
INSERT INTO share_allocation_windows(tenant_id,window_at,payload,payload_digest,created_by,operation_id) VALUES($1,$2,$3,$4,$5,$6);
-- name: GetShareAllocationQueue :many
SELECT * FROM share_allocation_queue WHERE tenant_id=$1 ORDER BY queued_at,node_id,kind;
-- name: SetShareAllocationQueue :exec
INSERT INTO share_allocation_queue(tenant_id,node_id,kind,queued_at,window_at,target,state) VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(tenant_id,node_id,kind) DO UPDATE SET queued_at=excluded.queued_at,window_at=excluded.window_at,target=excluded.target,state=excluded.state;
-- name: ShareDemandSamples :many
SELECT * FROM share_demand_samples WHERE tenant_id=$1 AND window_at=$2 ORDER BY node_id;
-- name: GetShareDemandSample :one
SELECT * FROM share_demand_samples WHERE tenant_id=$1 AND node_id=$2 AND window_at=$3;
-- name: AppendShareDemandSample :exec
INSERT INTO share_demand_samples(tenant_id,node_id,boot_id,sample_id,window_at,payload,rate_demand,connection_demand,rate_exhaustion_ppm,connection_exhaustion_ppm) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: ResetShareAllocationQueues :exec
UPDATE share_allocation_queue SET state='ineligible',target=0,window_at=$2 WHERE tenant_id=$1;

-- name: ShareBootstrapReserved :one
SELECT count(DISTINCT c.kind)::integer=2 AS reserved
FROM share_chains c JOIN share_generations g ON g.tenant_id=c.tenant_id AND g.node_id=c.node_id AND g.kind=c.kind AND g.generation=CASE WHEN c.pending_generation>0 THEN c.pending_generation ELSE c.active_generation END
WHERE c.tenant_id=$1 AND c.node_id=$2 AND c.boot_id=$3 AND g.boot_id=c.boot_id AND c.held_amount>0 AND g.amount>0
AND g.ownership_until>now() AND g.policy_until>now() AND g.autonomy_until>now()
AND (c.kind='connections' OR g.burst_bytes>0);
-- name: PreparedNodeDeployments :many
SELECT DISTINCT d.* FROM rule_deployments d JOIN rule_deployment_members m ON m.tenant_id=d.tenant_id AND m.deployment_id=d.id
JOIN rule_heads h ON h.tenant_id=d.tenant_id AND h.id=d.rule_id AND h.desired_revision=d.rule_revision
WHERE d.tenant_id=$1 AND m.node_id=$2 AND m.state='prepared' AND d.state='preparing' ORDER BY d.rule_id,d.id;
