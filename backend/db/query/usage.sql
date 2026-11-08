-- name: GetUsageTuple :one
SELECT * FROM usage_batches WHERE node_id=$1 AND boot_id=$2 AND sequence=$3;

-- name: GetUsageBatchID :one
SELECT * FROM usage_batches WHERE node_id=$1 AND boot_id=$2 AND batch_id=$3;

-- name: LockUsageTuple :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(tuple_key)::text,0));

-- name: AppendUsageBatch :exec
INSERT INTO usage_batches(tenant_id,node_id,boot_id,sequence,batch_id,billing_snapshot_id,payload_sha256,payload,raw_cumulative,billable_numerator,acknowledgement,facts,unknown_upper_bound) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);

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

-- name: UsageNodeHub :one
SELECT hub_id FROM nodes WHERE id=$1 AND lifecycle<>'disabled';

-- name: UsageGroupMember :one
SELECT EXISTS(SELECT 1 FROM node_group_members WHERE group_id=$1 AND node_id=$2);

-- name: AppendUsageConflict :exec
INSERT INTO usage_conflicts(node_id,boot_id,sequence,observed_digest,tenant_id,evidence) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING;

-- name: AppendUnknownLiability :exec
INSERT INTO unknown_liabilities(tenant_id,grant_id,billing_snapshot_id,confirmed_raw,unknown_raw,unknown_bill,evidence,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: GetUnknownLiability :one
SELECT * FROM unknown_liabilities WHERE tenant_id=$1 AND grant_id=$2;

-- name: FundRiskBudget :exec
INSERT INTO risk_budget_funding(id,amount,backing_reference,approved_by,approval_evidence,valid_until) VALUES($1,$2,$3,$4,$5,$6);

-- name: LockRiskBudget :one
SELECT * FROM risk_budget_funding WHERE id=$1 FOR UPDATE;

-- name: RiskBudgetUsed :one
SELECT coalesce(sum(amount),0)::bigint AS used FROM risk_budget_debits WHERE budget_id=$1;

-- name: DebitRiskBudget :exec
INSERT INTO risk_budget_debits(budget_id,tenant_id,grant_id,amount,compensation_source_id,evidence) VALUES($1,$2,$3,$4,$5,$6);

-- name: GrantRiskDebit :one
SELECT * FROM risk_budget_debits WHERE tenant_id=$1 AND grant_id=$2;

-- name: RecoverRiskBudget :exec
INSERT INTO risk_budget_recoveries(tenant_id,grant_id,amount,evidence) VALUES($1,$2,$3,$4);

-- name: AppendUsageReconciliationJob :exec
INSERT INTO usage_reconciliation_jobs(tenant_id,grant_id,task_id,next_attempt) VALUES($1,$2,$3,$4);

-- name: DueUsageReconciliation :many
SELECT j.* FROM usage_reconciliation_jobs j JOIN tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id WHERE j.next_attempt<=$1 AND t.state='pending' ORDER BY j.next_attempt,j.grant_id LIMIT 32;

-- name: LockUsageReconciliation :one
SELECT * FROM usage_reconciliation_jobs WHERE tenant_id=$1 AND grant_id=$2 FOR UPDATE;

-- name: BackoffUsageReconciliation :exec
UPDATE usage_reconciliation_jobs SET attempts=least(attempts+1,32),next_attempt=$3,alerted_at=CASE WHEN $4::boolean THEN coalesce(alerted_at,$5::timestamptz) ELSE alerted_at END WHERE tenant_id=$1 AND grant_id=$2;

-- name: ExpiredUsageReservations :many
SELECT r.* FROM reservations r WHERE r.valid_until<=$1 AND NOT EXISTS(SELECT 1 FROM unknown_liabilities u WHERE u.tenant_id=r.tenant_id AND u.grant_id=r.id) AND NOT EXISTS(SELECT 1 FROM settlements s WHERE s.tenant_id=r.tenant_id AND s.grant_id=r.id AND s.kind IN ('final','conservative')) ORDER BY r.valid_until,r.id LIMIT 32;
