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

-- name: DependentRuleHeads :many
SELECT h.* FROM rule_heads h WHERE h.tenant_id=$1 AND EXISTS(SELECT 1 FROM rule_dependencies d WHERE d.tenant_id=h.tenant_id AND d.rule_id=h.id AND d.revision=h.desired_revision AND d.dependency_kind=$2 AND d.dependency_id=$3) ORDER BY h.id FOR UPDATE;

-- name: SupersedePendingRuleCompiles :exec
UPDATE tasks t SET state='superseded' WHERE t.tenant_id=sqlc.arg(tenant_id) AND t.task_kind='rule_compile' AND t.state='pending' AND t.operation_id IN (SELECT o.id FROM operations o WHERE o.tenant_id=sqlc.arg(tenant_id) AND convert_from(o.payload,'UTF8')::jsonb->>'resource_id'=sqlc.arg(rule_id)::text);

-- name: InsertRuleCompileInput :exec
INSERT INTO rule_compile_inputs(tenant_id,rule_id,rule_revision,input_digest,canonical,billing_snapshot_id,signer_epoch,path_generation,fencing_generation)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING;

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
