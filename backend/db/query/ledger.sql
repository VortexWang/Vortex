-- name: EnsureSourceLedger :exec
INSERT INTO source_ledgers(tenant_id,source_id,origin) SELECT s.tenant_id,s.id,jsonb_build_object('package_snapshot_id',s.snapshot_id,'kind',s.kind,'period_key',s.period_key,'initial_amount',s.initial_amount,'issued_at',s.issued_at,'expires_at',s.expires_at) FROM traffic_sources s WHERE s.tenant_id=$1 AND s.id=$2 ON CONFLICT DO NOTHING;

-- name: EnsureLedgerProjection :exec
INSERT INTO ledger_projections(tenant_id,source_id) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: LockSourceProjection :one
SELECT * FROM ledger_projections WHERE tenant_id=$1 AND source_id=$2 FOR UPDATE;

-- name: GetSourceProjection :one
SELECT * FROM ledger_projections WHERE tenant_id=$1 AND source_id=$2;

-- name: GetLedgerEvent :one
SELECT * FROM ledger_events WHERE tenant_id=$1 AND source_id=$2 AND event_key=$3;

-- name: AppendLedgerEvent :one
INSERT INTO ledger_events(tenant_id,source_id,sequence,event_key,billing_snapshot_id,kind,delta_b,delta_f,delta_l,delta_charged,delta_remainder,evidence,previous_digest,digest) VALUES($1,$2,0,$3,$4,$5,$6,$7,$8,$9,$10,$11,''::bytea,''::bytea) RETURNING *;

-- name: RebuildLedgerProjection :one
SELECT coalesce(sum(delta_b),0)::bigint AS b,coalesce(sum(delta_f),0)::bigint AS f,coalesce(sum(delta_l),0)::bigint AS l,coalesce(sum(delta_charged),0)::bigint AS charged,coalesce(sum(delta_remainder),0)::bigint AS remainder_milli,count(*)::bigint AS sequence,verify_source_events($1,$2)::boolean AS verified FROM ledger_events WHERE tenant_id=$1 AND source_id=$2;

-- name: ListSourceProjections :many
SELECT * FROM ledger_projections WHERE tenant_id=$1 ORDER BY source_id;

-- name: MeteringSource :one
SELECT s.*,greatest(s.expires_at,coalesce((SELECT max(e.expires_at) FROM source_extensions e WHERE e.tenant_id=s.tenant_id AND e.source_id=s.id),s.expires_at))::timestamptz AS effective_until,EXISTS(SELECT 1 FROM source_expirations x WHERE x.tenant_id=s.tenant_id AND x.source_id=s.id AND x.effective_at<=sqlc.arg(at_time)) AS expired FROM traffic_sources s WHERE s.tenant_id=$1 AND s.id=$2 FOR UPDATE;

-- name: ScheduleBillingSnapshot :one
INSERT INTO billing_snapshots(id,tenant_id,package_snapshot_id,rule_id,rule_revision,path_generation,version,multiplier_milli,path_versions,valid_from,valid_until,announced_at,synchronized_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: RestoreSourceProjection :exec
UPDATE ledger_projections SET b=$3,f=$4,l=$5,charged=$6,remainder_milli=$7,sequence=$8,digest=$9 WHERE tenant_id=$1 AND source_id=$2;

-- name: LastSourceEvent :one
SELECT * FROM ledger_events WHERE tenant_id=$1 AND source_id=$2 ORDER BY sequence DESC LIMIT 1;

-- name: AppendPolicyBoundary :exec
INSERT INTO billing_policy_boundaries(tenant_id,old_snapshot_id,new_snapshot_id,boundary_at,evidence) VALUES($1,$2,$3,$4,$5);

-- name: SnapshotEffectiveUntil :one
SELECT least(b.valid_until,coalesce((SELECT p.boundary_at FROM billing_policy_boundaries p WHERE p.tenant_id=b.tenant_id AND p.old_snapshot_id=b.id),'infinity'::timestamptz))::timestamptz AS valid_until FROM billing_snapshots b WHERE b.tenant_id=$1 AND b.id=$2;
