-- name: GetUsageTuple :one
SELECT * FROM usage_batches WHERE node_id=$1 AND boot_id=$2 AND sequence=$3;

-- name: GetUsageBatchID :one
SELECT * FROM usage_batches WHERE node_id=$1 AND boot_id=$2 AND batch_id=$3;

-- name: LockUsageTuple :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(tuple_key)::text,0));

-- name: AppendUsageBatch :exec
INSERT INTO usage_batches(tenant_id,node_id,boot_id,sequence,batch_id,billing_snapshot_id,payload_sha256,payload,raw_cumulative,billable_numerator,acknowledgement,facts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);

-- name: AppendUsageReservation :one
INSERT INTO reservations(id,tenant_id,source_id,billing_snapshot_id,node_id,boot_id,request_nonce,raw_granted,coefficient_milli,reserved_bill,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: GetUsageReservation :one
SELECT * FROM reservations WHERE tenant_id=$1 AND id=$2;

-- name: FindUsageReservation :one
SELECT * FROM reservations WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3 AND request_nonce=$4;

-- name: ReservationAccounting :one
SELECT coalesce(max(raw_cumulative),0)::bigint AS raw_cumulative,coalesce(sum(returned_raw),0)::bigint AS returned_raw,coalesce(sum(charged_delta),0)::bigint AS charged,coalesce(sum(released_bill),0)::bigint AS released_bill,coalesce(bool_or(kind IN ('final','conservative')),false)::boolean AS terminal FROM settlements WHERE tenant_id=$1 AND grant_id=$2;

-- name: GetSettlementReceipt :one
SELECT * FROM settlements WHERE tenant_id=$1 AND grant_id=$2 AND receipt_key=$3;

-- name: AppendUsageSettlement :exec
INSERT INTO settlements(tenant_id,grant_id,billing_snapshot_id,kind,receipt_key,raw_cumulative,returned_raw,billable_numerator,evidence,charged_delta,released_bill) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);

-- name: AppendUsageDispute :exec
INSERT INTO disputes(id,tenant_id,grant_id,billing_snapshot_id,evidence_sha256,reason,evidence) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,grant_id,evidence_sha256) DO NOTHING;

-- name: WireBillingSnapshot :one
SELECT * FROM billing_snapshots WHERE id=$1;
