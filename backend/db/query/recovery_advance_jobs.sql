-- name: PendingRecoveryAdvanceJobs :many
WITH candidates AS MATERIALIZED (
 SELECT o.tenant_id,o.id,b.id AS sort_id,
 CASE WHEN t.task_kind='resource_change' THEN convert_from(o.payload,'UTF8')::jsonb ELSE '{}'::jsonb END AS body
 FROM tasks t JOIN operations o ON o.tenant_id=t.tenant_id AND o.id=t.operation_id
 JOIN outbox b ON b.tenant_id=o.tenant_id AND b.operation_id=o.id
 WHERE t.task_kind='resource_change' AND b.delivered_at IS NULL
 AND ((t.state='pending' AND (t.lease_until IS NULL OR t.lease_until<=now())) OR (t.state='leased' AND t.lease_until<now()))
)
SELECT c.tenant_id,c.id FROM candidates c JOIN platform_principals p ON p.id=c.body->>'actor_id'
WHERE p.state='active' AND p.role='platform_admin'
 AND c.body->>'action'='hub.advance.requested' AND c.body->>'advance_schema'='1'
ORDER BY c.sort_id LIMIT 32;

-- name: LockRecoveryAdvanceJob :one
SELECT t.* FROM tasks t WHERE t.tenant_id=$1 AND t.operation_id=$2 AND t.task_kind='resource_change' FOR UPDATE;

-- name: LeaseRecoveryAdvanceJob :execrows
UPDATE tasks SET state='leased',lease_token=$3,lease_until=now()+interval '30 seconds',attempts=attempts+1
WHERE tenant_id=$1 AND operation_id=$2 AND task_kind='resource_change'
AND ((state='pending' AND (lease_until IS NULL OR lease_until<=now())) OR (state='leased' AND lease_until<now()));

-- name: FinishRecoveryAdvanceJob :execrows
UPDATE tasks SET state=$4,lease_token=NULL,
 lease_until=CASE WHEN $4='pending' THEN now()+interval '1 second' ELSE NULL END
WHERE tenant_id=$1 AND operation_id=$2 AND task_kind='resource_change' AND state='leased' AND lease_token=$3 AND lease_until>now();
