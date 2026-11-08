-- Domain tasks deliberately use a separate desired-state identity, so public
-- operation DTOs select the original business resource/version from the typed
-- resource_change envelope. No raw payload or internal config identity escapes.
-- name: ListPublicOperations :many
SELECT o.id,
CASE WHEN t.task_kind='resource_change' THEN convert_from(o.payload,'UTF8')::jsonb->>'resource_id' ELSE o.resource_id END::text AS resource_id,
CASE WHEN t.task_kind='resource_change' THEN o.generation ELSE o.target_revision END::bigint AS target_revision,
o.state,o.occupation_held,o.public_created_at AS created_at
FROM operations o LEFT JOIN tasks t ON t.tenant_id=o.tenant_id AND t.operation_id=o.id
WHERE o.tenant_id=sqlc.arg(tenant_id)
AND (NOT sqlc.arg(has_cursor)::boolean OR (o.public_created_at,o.id)>(sqlc.arg(after_created_at)::timestamptz,sqlc.arg(after_id)::text))
ORDER BY o.public_created_at,o.id LIMIT sqlc.arg(page_limit)::integer;

-- name: GetPublicOperation :one
SELECT o.id,
CASE WHEN t.task_kind='resource_change' THEN convert_from(o.payload,'UTF8')::jsonb->>'resource_id' ELSE o.resource_id END::text AS resource_id,
CASE WHEN t.task_kind='resource_change' THEN o.generation ELSE o.target_revision END::bigint AS target_revision,
o.state,o.occupation_held,o.public_created_at AS created_at
FROM operations o LEFT JOIN tasks t ON t.tenant_id=o.tenant_id AND t.operation_id=o.id
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(id);
