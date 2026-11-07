-- name: InsertTenant :one
INSERT INTO tenants(id) VALUES($1) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id=sqlc.arg(tenant_id);

-- name: LockTenant :one
SELECT * FROM tenants WHERE id=sqlc.arg(tenant_id) FOR UPDATE;

-- name: SetTenantState :one
UPDATE tenants SET account_state=sqlc.arg(account_state),version=version+1 WHERE id=sqlc.arg(tenant_id) AND version=sqlc.arg(expected_version) RETURNING *;

-- name: GetTenantUser :one
SELECT * FROM users WHERE tenant_id=$1 AND id=$2;

-- name: GetPlatformPrincipal :one
SELECT * FROM platform_principals WHERE id=$1;

-- name: InsertTenantUser :exec
INSERT INTO users(id,tenant_id,role,state) VALUES($1,$2,$3,$4);

-- name: InsertResourceOwnership :exec
INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind,owner_tenant_id) VALUES($1,$2,$3,$4);

-- name: InsertResourceGrant :exec
INSERT INTO resource_grants(id,tenant_id,resource_id,version,status,valid_from,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: GetPlatformResource :one
SELECT * FROM resource_ownership WHERE resource_id=$1 AND owner_kind='platform';

-- name: ListTenantResources :many
SELECT r.* FROM resource_ownership r WHERE r.owner_tenant_id=sqlc.arg(tenant_id) OR
(r.owner_kind='platform' AND EXISTS(SELECT 1 FROM resource_grants g WHERE g.resource_id=r.resource_id AND g.tenant_id=sqlc.arg(tenant_id) AND g.status='active' AND g.valid_from<=sqlc.arg(at_time) AND g.valid_until>sqlc.arg(at_time))) ORDER BY r.resource_id;

-- name: AppendRestrictionEvent :one
INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at,cleared_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: GetRestrictionEvent :one
SELECT * FROM restriction_events WHERE tenant_id=$1 AND cause=$2 AND source_id=$3 AND source_version=$4 AND event_kind=$5;

-- name: ApplyCurrentRestriction :execrows
INSERT INTO current_restrictions(tenant_id,cause,source_id,source_version,event_id,effective_at)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(tenant_id,cause,source_id) DO UPDATE SET source_version=excluded.source_version,event_id=excluded.event_id,effective_at=excluded.effective_at,cleared_at=NULL
WHERE current_restrictions.source_version<excluded.source_version;

-- name: ClearCurrentRestriction :execrows
UPDATE current_restrictions SET cleared_at=$5,event_id=$6
WHERE tenant_id=$1 AND cause=$2 AND source_id=$3 AND source_version=$4 AND cleared_at IS NULL AND effective_at<=$5;

-- name: GetCurrentRestriction :one
SELECT * FROM current_restrictions WHERE tenant_id=$1 AND cause=$2 AND source_id=$3;

-- name: ListCurrentRestrictions :many
SELECT * FROM current_restrictions WHERE tenant_id=$1 AND resource_id IS NULL AND cleared_at IS NULL AND effective_at<=sqlc.arg(at_time) ORDER BY cause,source_id;

-- name: ListRestrictionHistory :many
SELECT * FROM restriction_events WHERE tenant_id=$1 ORDER BY cause,source_id,source_version,event_kind;

-- name: ListTenantUsers :many
SELECT * FROM users WHERE tenant_id=$1 ORDER BY id;

-- name: ListTenantGrants :many
SELECT * FROM resource_grants WHERE tenant_id=$1 ORDER BY id;

-- name: ListTenantTasks :many
SELECT * FROM tasks WHERE tenant_id=$1 ORDER BY id;
