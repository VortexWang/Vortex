-- name: InsertNodeGroup :one
INSERT INTO node_groups(id,name,purpose,scheduling,health_interval_seconds,health_timeout_seconds,billing_multiplier_milli)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: ScopedGroup :one
SELECT g.*,o.owner_kind,o.owner_tenant_id FROM node_groups g JOIN resource_ownership o ON o.resource_id=g.id
WHERE g.id=sqlc.arg(group_id) AND g.deleted_at IS NULL AND
(sqlc.arg(platform_read)::boolean OR o.owner_tenant_id=sqlc.arg(tenant_id)::text OR EXISTS(SELECT 1 FROM resource_grants r WHERE r.resource_id=g.id AND r.tenant_id=sqlc.arg(tenant_id) AND r.status='active' AND r.valid_from<=now() AND r.valid_until>now())) FOR UPDATE OF g;

-- name: GroupMembers :many
SELECT n.*,m.weight,m.priority FROM node_group_members m JOIN nodes n ON n.id=m.node_id WHERE m.group_id=$1 ORDER BY n.id FOR UPDATE OF n;

-- name: ClearGroupMembers :exec
DELETE FROM node_group_members WHERE group_id=$1;

-- name: AddGroupMember :exec
INSERT INTO node_group_members(group_id,node_id,weight,priority) VALUES($1,$2,$3,$4);

-- name: AdvanceGroupMembers :one
UPDATE node_groups SET member_version=member_version+1,version=version+1 WHERE id=$1 AND version=$2 RETURNING *;

-- name: PutSegmentPermit :one
INSERT INTO segment_permits(tenant_id,from_group_id,to_group_id,version,allowed) VALUES($1,$2,$3,1,$4)
ON CONFLICT(tenant_id,from_group_id,to_group_id) DO UPDATE SET version=segment_permits.version+1,allowed=excluded.allowed WHERE segment_permits.version=sqlc.arg(expected_version) RETURNING *;

-- name: GetSegmentPermit :one
SELECT * FROM segment_permits WHERE tenant_id=$1 AND from_group_id=$2 AND to_group_id=$3;
