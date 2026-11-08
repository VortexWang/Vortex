-- Domain tasks deliberately use a separate desired-state identity, so public
-- operation DTOs select the original business resource/version from the typed
-- domain envelope. No raw payload or internal config identity escapes.
-- name: ListPublicOperations :many
SELECT o.id,
CASE WHEN t.task_kind<>'control' THEN coalesce(convert_from(o.payload,'UTF8')::jsonb->>'resource_id',CASE WHEN convert_from(o.payload,'UTF8')::jsonb->>'action'='dns.reconcile' THEN convert_from(o.payload,'UTF8')::jsonb->>'group_id' END,o.resource_id) ELSE o.resource_id END::text AS resource_id,
CASE WHEN t.task_kind<>'control' THEN o.generation ELSE o.target_revision END::bigint AS target_revision,
o.state,o.occupation_held,o.public_created_at AS created_at
FROM operations o LEFT JOIN tasks t ON t.tenant_id=o.tenant_id AND t.operation_id=o.id
WHERE o.tenant_id=sqlc.arg(tenant_id)
AND (NOT sqlc.arg(has_cursor)::boolean OR (o.public_created_at,o.id)>(sqlc.arg(after_created_at)::timestamptz,sqlc.arg(after_id)::text))
ORDER BY o.public_created_at,o.id LIMIT sqlc.arg(page_limit)::integer;

-- name: GetPublicOperation :one
SELECT o.id,
CASE WHEN t.task_kind<>'control' THEN coalesce(convert_from(o.payload,'UTF8')::jsonb->>'resource_id',CASE WHEN convert_from(o.payload,'UTF8')::jsonb->>'action'='dns.reconcile' THEN convert_from(o.payload,'UTF8')::jsonb->>'group_id' END,o.resource_id) ELSE o.resource_id END::text AS resource_id,
CASE WHEN t.task_kind<>'control' THEN o.generation ELSE o.target_revision END::bigint AS target_revision,
o.state,o.occupation_held,o.public_created_at AS created_at
FROM operations o LEFT JOIN tasks t ON t.tenant_id=o.tenant_id AND t.operation_id=o.id
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(id);
