-- The immutable internal actor binding is emitted only for platform-owned
-- group jobs. Historical actorless hints do not grant worker authority.
-- name: PendingGroupRevalidations :many
WITH candidates AS MATERIALIZED (
 SELECT o.tenant_id,o.id,b.id AS sort_id,
 CASE WHEN t.task_kind='resource_change' THEN convert_from(o.payload,'UTF8')::jsonb ELSE '{}'::jsonb END AS body
 FROM tasks t JOIN operations o ON o.tenant_id=t.tenant_id AND o.id=t.operation_id
 JOIN outbox b ON b.tenant_id=o.tenant_id AND b.operation_id=o.id
 WHERE t.task_kind='resource_change' AND t.state='pending' AND o.state='pending' AND b.delivered_at IS NULL
)
SELECT c.tenant_id,c.id FROM candidates c JOIN platform_principals p ON p.id=c.body->>'actor_id'
WHERE p.state='active' AND p.role IN ('platform_admin','operator')
 AND c.body->>'action'='group.revalidate_all_rules'
 AND c.body->>'revalidation_schema'='1' AND c.body->>'platform_actor'='true'
ORDER BY c.sort_id LIMIT 32;

-- name: GroupDependentTenants :many
SELECT DISTINCT h.tenant_id FROM rule_heads h WHERE EXISTS(
 SELECT 1 FROM rule_dependencies d WHERE d.tenant_id=h.tenant_id AND d.rule_id=h.id
 AND d.revision=h.desired_revision AND d.dependency_kind='group' AND d.dependency_id=$1)
ORDER BY h.tenant_id;

-- No group lock is acquired while invalidating rule heads. New group changes
-- enqueue their own job; newer rules already resolve the committed membership.
-- name: GetGroupRevalidationAuthority :one
SELECT g.id,g.version FROM node_groups g JOIN resource_ownership o ON o.resource_id=g.id
WHERE g.id=$1 AND g.deleted_at IS NULL AND o.resource_kind='node_group' AND o.owner_kind='platform';

-- name: LockGroupRevalidationTask :one
SELECT * FROM tasks WHERE tenant_id=$1 AND operation_id=$2 AND task_kind='resource_change' FOR UPDATE;
