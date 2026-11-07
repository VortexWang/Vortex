-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE tenant_id=$1;
-- name: LockSubscription :one
SELECT * FROM subscriptions WHERE tenant_id=$1 FOR UPDATE;
-- name: CreateSubscription :one
INSERT INTO subscriptions(id,tenant_id,snapshot_id,anchor_at,expires_at) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: ReplaceSubscription :one
UPDATE subscriptions SET snapshot_id=$2,anchor_at=$3,expires_at=$4,version=version+1 WHERE tenant_id=$1 AND version=$5 RETURNING *;
-- name: ExtendSubscription :one
UPDATE subscriptions SET expires_at=$2,version=version+1 WHERE tenant_id=$1 AND version=$3 RETURNING *;
-- name: AppendTrafficSource :one
INSERT INTO traffic_sources(id,tenant_id,snapshot_id,subscription_id,kind,initial_amount,issued_at,expires_at,period_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;
-- name: ListTrafficSources :many
SELECT * FROM traffic_sources WHERE tenant_id=$1 ORDER BY CASE kind WHEN 'base' THEN 0 ELSE 1 END,expires_at,issued_at,id;
-- name: ExpireSource :exec
INSERT INTO source_expirations(tenant_id,source_id,effective_at,reason) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: SourceIsExpired :one
SELECT EXISTS(SELECT 1 FROM source_expirations WHERE tenant_id=$1 AND source_id=$2 AND effective_at<=sqlc.arg(at_time));
-- name: AppendSourceExtension :exec
INSERT INTO source_extensions(tenant_id,source_id,request_key,expires_at) VALUES($1,$2,$3,$4);
-- name: SourceEffectiveExpiry :one
SELECT GREATEST(s.expires_at,COALESCE((SELECT max(e.expires_at) FROM source_extensions e WHERE e.tenant_id=s.tenant_id AND e.source_id=s.id),s.expires_at))::timestamptz FROM traffic_sources s WHERE s.tenant_id=$1 AND s.id=$2;
-- name: TenantPackageRules :many
SELECT h.id,h.desired_revision,r.payload,r.created_at FROM rule_heads h JOIN rule_revisions r ON r.tenant_id=h.tenant_id AND r.rule_id=h.id AND r.revision=h.desired_revision WHERE h.tenant_id=$1 ORDER BY r.created_at,h.id;
-- name: TenantPackagePorts :many
SELECT * FROM port_allocations WHERE tenant_id=$1 AND state NOT IN ('released','aborted') ORDER BY id;
-- name: AppendPackageChange :one
INSERT INTO package_changes(id,tenant_id,subscription_id,snapshot_id,expected_version,effective_at,preview,created_by,task_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;
-- name: DuePackageChanges :many
SELECT * FROM package_changes WHERE state='pending' AND effective_at<=now() ORDER BY effective_at,id LIMIT 32;
-- name: LockPackageChange :one
SELECT * FROM package_changes WHERE tenant_id=$1 AND id=$2 FOR UPDATE;
-- name: FinishPackageChange :exec
UPDATE package_changes SET state=$3 WHERE tenant_id=$1 AND id=$2 AND state='pending';
-- name: ExpiringPackageTenants :many
SELECT tenant_id FROM subscriptions WHERE expires_at<=now() ORDER BY tenant_id;
-- name: LivePackageTenants :many
SELECT tenant_id FROM subscriptions WHERE expires_at>now() ORDER BY tenant_id;
