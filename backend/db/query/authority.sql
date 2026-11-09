-- name: GetDesiredState :one
SELECT * FROM desired_state WHERE tenant_id=$1 AND resource_id=$2;

-- name: PutDesiredState :one
INSERT INTO desired_state(tenant_id,resource_id,target_kind,revision,payload,payload_sha256)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(tenant_id,resource_id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload,payload_sha256=excluded.payload_sha256
WHERE desired_state.revision=sqlc.arg(expected_revision) AND desired_state.target_kind=excluded.target_kind
RETURNING *;

-- name: GetOperation :one
SELECT * FROM operations WHERE tenant_id=$1 AND id=$2;

-- name: GetOperationByKey :one
SELECT * FROM operations WHERE tenant_id=$1 AND idempotency_key=$2;

-- name: InsertOperation :one
INSERT INTO operations(id,tenant_id,resource_id,idempotency_key,request_sha256,target_kind,target_revision,expected_revision,generation,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: InsertTask :exec
INSERT INTO tasks(id,tenant_id,operation_id) VALUES($1,$2,$3);

-- name: InsertOutbox :exec
INSERT INTO outbox(tenant_id,operation_id) VALUES($1,$2);

-- name: InsertAudit :exec
INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,request_id,before_summary,after_summary,reason)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: LeaseTask :one
UPDATE tasks SET state='leased',lease_token=sqlc.arg(lease_token),lease_until=now()+interval '10 seconds',attempts=attempts+1
WHERE id=(SELECT t.id FROM tasks t JOIN outbox o ON o.operation_id=t.operation_id
WHERE t.state='pending' AND t.task_kind='control' AND o.delivered_at IS NULL ORDER BY o.id FOR UPDATE OF t SKIP LOCKED LIMIT 1)
RETURNING *;

-- name: MarkRunning :execrows
UPDATE tasks SET state='running' WHERE tenant_id=$1 AND operation_id=$2 AND lease_token=$3 AND state='leased' AND lease_until>now();

-- name: UpdateOperationState :exec
UPDATE operations SET state=$3 WHERE tenant_id=$1 AND id=$2;

-- name: FinishTask :execrows
UPDATE tasks SET state=$4,lease_until=NULL WHERE tenant_id=$1 AND operation_id=$2 AND lease_token=$3 AND state IN ('leased','running');

-- name: MarkDelivered :exec
UPDATE outbox SET delivered_at=now() WHERE tenant_id=$1 AND operation_id=$2;

-- name: GetReceipt :one
SELECT * FROM control_receipts WHERE tenant_id=$1 AND operation_id=$2;

-- name: InsertReceipt :exec
INSERT INTO control_receipts(tenant_id,operation_id,target_revision,generation,receipt_sha256,state) VALUES($1,$2,$3,$4,$5,$6);

-- name: ExpireLeases :many
UPDATE tasks SET state='unknown',lease_until=NULL WHERE id IN
(SELECT id FROM tasks WHERE task_kind='control' AND state IN ('leased','running') AND lease_until<now() ORDER BY lease_until FOR UPDATE SKIP LOCKED LIMIT 100) RETURNING *;

-- name: LockOperation :one
SELECT * FROM operations WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: ListAuthority :many
SELECT id,tenant_id,resource_id,target_revision,state,occupation_held FROM operations WHERE tenant_id=$1 ORDER BY id;

-- name: InsertControlObservation :exec
INSERT INTO control_observations(hub_id,session_id,payload) VALUES($1,$2,$3);

-- name: LockTask :one
SELECT id,tenant_id,operation_id,state,lease_token,lease_until,attempts FROM tasks WHERE tenant_id=$1 AND operation_id=$2 FOR UPDATE;

-- name: ReconcileTask :exec
UPDATE tasks SET state=$3,lease_until=NULL WHERE tenant_id=$1 AND operation_id=$2;
