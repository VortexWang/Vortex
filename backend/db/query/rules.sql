-- name: EnsureRuleHead :exec
INSERT INTO rule_heads(tenant_id,id) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: LockRuleHead :one
SELECT * FROM rule_heads WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: GetRuleRevision :one
SELECT * FROM rule_revisions WHERE tenant_id=$1 AND rule_id=$2 AND revision=$3;

-- name: AppendRuleRevision :one
INSERT INTO rule_revisions(tenant_id,rule_id,revision,payload,payload_sha256,created_by,change_class,rollback_of)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: AdvanceRuleHead :execrows
UPDATE rule_heads SET desired_revision=desired_revision+1,sync_status='pending' WHERE tenant_id=$1 AND id=$2 AND desired_revision=sqlc.arg(expected_revision);

-- name: InsertRuleTarget :exec
INSERT INTO rule_targets(tenant_id,rule_id,revision,address,weight,priority) VALUES($1,$2,$3,$4,$5,$6);

-- name: InsertRuleSegment :exec
INSERT INTO rule_segments(tenant_id,rule_id,revision,hop,from_group_id,to_group_id,direction,permit_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: InsertRuleDependency :exec
INSERT INTO rule_dependencies(tenant_id,rule_id,revision,dependency_kind,dependency_id,version) VALUES($1,$2,$3,$4,$5,$6);

-- name: CopyStopRuleSegments :exec
INSERT INTO rule_segments(tenant_id,rule_id,revision,hop,from_group_id,to_group_id,direction,permit_version)
SELECT r.tenant_id,r.rule_id,sqlc.arg(new_revision)::bigint,r.hop,r.from_group_id,r.to_group_id,r.direction,r.permit_version
FROM rule_segments r WHERE r.tenant_id=$1 AND r.rule_id=$2 AND r.revision=sqlc.arg(old_revision)::bigint;

-- name: CopyStopRuleDependencies :exec
INSERT INTO rule_dependencies(tenant_id,rule_id,revision,dependency_kind,dependency_id,version)
SELECT r.tenant_id,r.rule_id,sqlc.arg(new_revision)::bigint,r.dependency_kind,r.dependency_id,r.version
FROM rule_dependencies r WHERE r.tenant_id=$1 AND r.rule_id=$2 AND r.revision=sqlc.arg(old_revision)::bigint;

-- name: DependentRuleHeads :many
SELECT h.* FROM rule_heads h WHERE h.tenant_id=$1 AND EXISTS(SELECT 1 FROM rule_dependencies d WHERE d.tenant_id=h.tenant_id AND d.rule_id=h.id AND d.revision=h.desired_revision AND d.dependency_kind=$2 AND d.dependency_id=$3) ORDER BY h.id FOR UPDATE;

-- name: SupersedePendingRuleCompiles :exec
UPDATE tasks t SET state='superseded' WHERE t.tenant_id=sqlc.arg(tenant_id) AND t.task_kind='rule_compile' AND t.state='pending' AND EXISTS(SELECT 1 FROM rule_compile_requests r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.rule_id=sqlc.arg(rule_id));

-- name: BindRuleCompileTask :exec
INSERT INTO rule_compile_requests(tenant_id,task_id,rule_id,rule_revision) VALUES($1,$2,$3,$4);

-- name: InsertRuleCompileInput :exec
INSERT INTO rule_compile_inputs(tenant_id,rule_id,rule_revision,input_digest,canonical,billing_snapshot_id,signer_epoch,path_generation,fencing_generation)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING;

-- name: LockRuleCompileRequest :one
SELECT r.*,t.state FROM rule_compile_requests r JOIN tasks t ON t.tenant_id=r.tenant_id AND t.id=r.task_id WHERE r.tenant_id=$1 AND r.task_id=$2 AND t.task_kind='rule_compile' FOR UPDATE OF t,r;

-- name: GetRuleCompileRequest :one
SELECT * FROM rule_compile_requests WHERE tenant_id=$1 AND task_id=$2;

-- name: BindRuleCompileInput :execrows
UPDATE rule_compile_requests SET input_digest=$3 WHERE tenant_id=$1 AND task_id=$2 AND input_digest IS NULL;

-- name: ReadyRuleCompiles :many
SELECT r.tenant_id,r.task_id,v.created_by FROM rule_compile_requests r JOIN tasks t ON t.tenant_id=r.tenant_id AND t.id=r.task_id JOIN rule_revisions v ON v.tenant_id=r.tenant_id AND v.rule_id=r.rule_id AND v.revision=r.rule_revision JOIN rule_heads h ON h.tenant_id=r.tenant_id AND h.id=r.rule_id JOIN outbox o ON o.tenant_id=t.tenant_id AND o.operation_id=t.operation_id WHERE t.state='pending' AND t.task_kind='rule_compile' AND r.input_digest IS NOT NULL AND h.desired_revision=r.rule_revision AND o.delivered_at IS NULL ORDER BY o.id LIMIT 100;

-- name: InsertRuleArtifact :exec
INSERT INTO rule_artifacts(tenant_id,rule_id,rule_revision,node_id,input_digest,digest,canonical,billing_snapshot_id,signer_epoch,path_generation,fencing_generation)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING;

-- name: CommitRuleArtifact :exec
INSERT INTO rule_artifact_commits(tenant_id,digest) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: GetCommittedRuleArtifact :one
SELECT a.* FROM rule_artifacts a JOIN rule_artifact_commits c ON c.tenant_id=a.tenant_id AND c.digest=a.digest
WHERE a.tenant_id=$1 AND a.digest=$2 AND a.rule_id=$3 AND a.rule_revision=$4 AND a.node_id=$5 AND a.fencing_generation=$6;

-- name: GetRuleCompileInput :one
SELECT * FROM rule_compile_inputs WHERE tenant_id=$1 AND rule_id=$2 AND rule_revision=$3 AND input_digest=$4;

-- name: ListCommittedRuleArtifacts :many
SELECT a.* FROM rule_artifacts a JOIN rule_artifact_commits c ON c.tenant_id=a.tenant_id AND c.digest=a.digest WHERE a.tenant_id=$1 AND a.rule_id=$2 AND a.rule_revision=$3 AND a.input_digest=$4 ORDER BY a.node_id;

-- name: InsertRuleDeployment :one
INSERT INTO rule_deployments(tenant_id,id,rule_id,rule_revision,input_digest,state,deadline,created_by) VALUES($1,$2,$3,$4,$5,'preparing',now()+interval '120 seconds',$6) RETURNING *;

-- name: LockRuleDeployment :one
SELECT * FROM rule_deployments WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: GetRuleDeployment :one
SELECT * FROM rule_deployments WHERE tenant_id=$1 AND id=$2;

-- name: RuleDeploymentByInput :one
SELECT * FROM rule_deployments WHERE tenant_id=$1 AND rule_id=$2 AND rule_revision=$3 AND input_digest=$4;

-- name: InsertRuleDeploymentMember :exec
INSERT INTO rule_deployment_members(tenant_id,deployment_id,node_id,boot_id,transaction_id,artifact_digest,fencing_generation) VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: RuleDeploymentMembers :many
SELECT * FROM rule_deployment_members WHERE tenant_id=$1 AND deployment_id=$2 ORDER BY node_id FOR UPDATE;

-- name: SetRuleMemberState :exec
UPDATE rule_deployment_members SET state=$4 WHERE tenant_id=$1 AND deployment_id=$2 AND node_id=$3;

-- name: SetRuleDeploymentState :exec
UPDATE rule_deployments SET state=$3 WHERE tenant_id=$1 AND id=$2;

-- name: SetRuleSync :exec
UPDATE rule_heads SET sync_status=$4,active_revision=CASE WHEN $4::text='running' THEN $3 ELSE active_revision END WHERE tenant_id=$1 AND id=$2 AND desired_revision=$3;

-- name: BindRuleDelivery :exec
INSERT INTO rule_delivery_bindings(tenant_id,task_id,deployment_id,node_id,phase) VALUES($1,$2,$3,$4,$5);

-- name: RuleDelivery :one
SELECT b.*,d.rule_id,d.rule_revision,d.input_digest,m.boot_id,m.transaction_id,m.artifact_digest,m.fencing_generation,m.state AS member_state,o.idempotency_key FROM rule_delivery_bindings b JOIN rule_deployments d ON d.tenant_id=b.tenant_id AND d.id=b.deployment_id JOIN rule_deployment_members m ON m.tenant_id=b.tenant_id AND m.deployment_id=b.deployment_id AND m.node_id=b.node_id JOIN operations o ON o.tenant_id=b.tenant_id AND o.id=b.task_id WHERE b.tenant_id=$1 AND b.task_id=$2;

-- name: LeaseRuleDelivery :one
UPDATE tasks SET state='leased',lease_token=sqlc.arg(lease_token),lease_until=now()+interval '10 seconds',attempts=attempts+1 WHERE id=(SELECT t.id FROM tasks t JOIN rule_delivery_bindings b ON b.tenant_id=t.tenant_id AND b.task_id=t.id JOIN outbox o ON o.tenant_id=t.tenant_id AND o.operation_id=t.operation_id JOIN operations op ON op.tenant_id=t.tenant_id AND op.id=t.operation_id
JOIN rule_deployments d ON d.tenant_id=b.tenant_id AND d.id=b.deployment_id
JOIN rule_revisions r ON r.tenant_id=d.tenant_id AND r.rule_id=d.rule_id AND r.revision=d.rule_revision
WHERE t.state='pending' AND o.delivered_at IS NULL AND (b.phase<>'query' OR op.created_at<=now()-interval '1 second')
AND (b.phase IN ('query','abort') OR NOT (convert_from(r.payload,'UTF8')::jsonb->>'enabled')::boolean OR (
 EXISTS(SELECT 1 FROM tenants a WHERE a.id=t.tenant_id AND a.account_state='active')
 AND NOT EXISTS(SELECT 1 FROM current_restrictions cr WHERE cr.tenant_id=t.tenant_id AND (cr.resource_id IS NULL OR cr.resource_id=d.rule_id) AND cr.cleared_at IS NULL AND cr.effective_at<=now())))
ORDER BY o.id FOR UPDATE OF t SKIP LOCKED LIMIT 1) RETURNING *;

-- name: InsertRuleReceipt :exec
INSERT INTO rule_delivery_receipts(tenant_id,task_id,receipt_sha256,payload) VALUES($1,$2,$3,$4);

-- name: GetRuleReceipt :one
SELECT * FROM rule_delivery_receipts WHERE tenant_id=$1 AND task_id=$2;

-- name: PendingRuleDelivery :one
SELECT EXISTS(SELECT 1 FROM rule_delivery_bindings b JOIN tasks t ON t.tenant_id=b.tenant_id AND t.id=b.task_id WHERE b.tenant_id=$1 AND b.deployment_id=$2 AND b.node_id=$3 AND b.phase=$4 AND t.state IN ('pending','leased','running'));

-- name: ExpiredRuleDeployments :many
SELECT * FROM rule_deployments WHERE deadline<=now() AND state IN ('preparing','committing','partial','unknown') AND rollback_revision=0 ORDER BY deadline LIMIT 100;

-- name: SetRuleRollbackRevision :exec
UPDATE rule_deployments SET rollback_revision=$3 WHERE tenant_id=$1 AND id=$2;

-- name: SupersedePendingRuleDeliveries :many
UPDATE tasks t SET state='superseded' WHERE t.tenant_id=sqlc.arg(tenant_id) AND t.state='pending' AND EXISTS(SELECT 1 FROM rule_delivery_bindings b JOIN rule_deployments d ON d.tenant_id=b.tenant_id AND d.id=b.deployment_id WHERE b.tenant_id=t.tenant_id AND b.task_id=t.id AND d.rule_id=sqlc.arg(rule_id) AND d.rule_revision<sqlc.arg(target_revision) AND b.phase IN ('prepare','commit')) RETURNING t.*;

-- name: ExpireRuleLeases :exec
UPDATE tasks t SET state='unknown',lease_until=NULL WHERE t.state IN ('leased','running') AND t.lease_until<now() AND EXISTS(SELECT 1 FROM rule_delivery_bindings b WHERE b.tenant_id=t.tenant_id AND b.task_id=t.id);

-- name: UnobservedRuleUnknowns :many
SELECT t.* FROM tasks t JOIN rule_delivery_bindings b ON b.tenant_id=t.tenant_id AND b.task_id=t.id WHERE t.state='unknown' AND NOT EXISTS(SELECT 1 FROM rule_delivery_receipts r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id) ORDER BY t.id LIMIT 100;
