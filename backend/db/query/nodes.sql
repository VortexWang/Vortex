-- name: CreateOwnedResource :exec
INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind,owner_tenant_id) VALUES($1,$2,$3,$4);

-- name: InsertNode :one
INSERT INTO nodes(id,name,region,hub_id,capabilities,public_address,program_version,port_min,port_max,link_port,control_port,metrics_port,capacity)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: ScopedNode :one
SELECT n.*,o.owner_kind,o.owner_tenant_id FROM nodes n JOIN resource_ownership o ON o.resource_id=n.id
WHERE n.id=sqlc.arg(node_id) AND n.deleted_at IS NULL AND
(sqlc.arg(platform_read)::boolean OR o.owner_tenant_id=sqlc.arg(tenant_id)::text OR EXISTS(SELECT 1 FROM resource_grants g WHERE g.resource_id=n.id AND g.tenant_id=sqlc.arg(tenant_id) AND g.status='active' AND g.valid_from<=now() AND g.valid_until>now())) FOR UPDATE OF n;

-- name: SetNodeLifecycle :one
UPDATE nodes SET lifecycle=$2,drain_deadline=$3,version=version+1 WHERE id=$1 AND version=$4 AND deleted_at IS NULL RETURNING *;

-- name: DeleteNode :execrows
UPDATE nodes SET deleted_at=now(),version=version+1 WHERE id=$1 AND version=$2 AND lifecycle='disabled' AND deleted_at IS NULL;

-- name: CountNodeDependencies :one
SELECT (SELECT count(*) FROM node_group_members m WHERE m.node_id=sqlc.arg(node_id)) + (SELECT count(*) FROM node_dependencies d WHERE d.node_id=sqlc.arg(node_id)) AS count;

-- name: NodeImpact :many
SELECT * FROM node_dependencies WHERE node_id=$1 ORDER BY kind,dependency_id;

-- name: InsertDomainTask :exec
INSERT INTO tasks(id,tenant_id,operation_id,task_kind) VALUES($1,$2,$3,$4);

-- name: LockResourceOwnership :one
SELECT * FROM resource_ownership WHERE resource_id=$1 FOR UPDATE;

-- name: LatestGrant :one
SELECT * FROM resource_grants WHERE tenant_id=$1 AND resource_id=$2 ORDER BY version DESC LIMIT 1 FOR UPDATE;

-- name: RevokeResourceGrant :exec
UPDATE resource_grants SET status='revoked' WHERE tenant_id=$1 AND resource_id=$2 AND status='active';

-- name: AppendGrantHistory :exec
INSERT INTO grant_history(grant_id,tenant_id,resource_id,version,status,valid_from,valid_until)
SELECT r.id,r.tenant_id,r.resource_id,r.version,r.status,r.valid_from,r.valid_until FROM resource_grants r WHERE r.tenant_id=$1 AND r.id=$2 ON CONFLICT(grant_id,status) DO NOTHING;
