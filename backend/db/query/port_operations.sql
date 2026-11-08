-- name: InsertPortOperationBinding :exec
INSERT INTO port_operation_bindings(tenant_id,allocation_id,operation_id,action,allocation_version,actor_id,expected_reservations)
VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: GetPortOperationBinding :one
SELECT * FROM port_operation_bindings WHERE tenant_id=$1 AND operation_id=$2;

-- name: GetPortOperationProjection :one
SELECT desired_state,desired_held,actual_state,actual_held FROM port_operation_projection WHERE tenant_id=$1 AND operation_id=$2;

-- name: ReadyPortOperationProjections :many
SELECT p.tenant_id,p.operation_id FROM port_operation_projection p JOIN operations o ON o.tenant_id=p.tenant_id AND o.id=p.operation_id
WHERE p.actual_state<>p.desired_state OR p.actual_held<>p.desired_held
ORDER BY o.public_created_at,o.id LIMIT 32;

-- name: SetPortOperationProjection :exec
UPDATE operations SET state=$3,occupation_held=$4 WHERE tenant_id=$1 AND id=$2;

-- name: LockPortOperationTask :one
SELECT * FROM tasks WHERE tenant_id=$1 AND operation_id=$2 AND task_kind='resource_change' FOR UPDATE;
