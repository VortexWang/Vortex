-- name: ListPublicOperations :many
SELECT id,resource_id,target_revision,state,occupation_held,public_created_at AS created_at
FROM operations WHERE tenant_id=sqlc.arg(tenant_id)
AND (NOT sqlc.arg(has_cursor)::boolean OR (public_created_at,id)>(sqlc.arg(after_created_at)::timestamptz,sqlc.arg(after_id)::text))
ORDER BY public_created_at,id LIMIT sqlc.arg(page_limit)::integer;

-- name: GetPublicOperation :one
SELECT id,resource_id,target_revision,state,occupation_held,public_created_at AS created_at
FROM operations WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id);
