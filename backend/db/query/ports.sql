-- name: InsertPortAllocation :one
INSERT INTO port_allocations(id,tenant_id,rule_id,group_id,member_set_version,port,listen_address,protocols,purpose,state)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'preparing') RETURNING *;

-- name: GetPortAllocation :one
SELECT * FROM port_allocations WHERE tenant_id=$1 AND id=$2;

-- name: RuleCompilePortAllocations :many
SELECT * FROM port_allocations WHERE tenant_id=$1 AND rule_id=$2 AND group_id=$3
AND state IN ('preparing','committed','retained') AND needs_revalidation=false ORDER BY id FOR UPDATE;

-- name: BindRulePortPrepare :exec
UPDATE port_reservations SET prepare_task_id=$4,config_confirmed=false,state='prepared'
WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND state<>'released';

-- name: SupersedeUnsentPortTask :execrows
UPDATE tasks SET state='superseded' WHERE tenant_id=$1 AND id=$2 AND task_kind='port_prepare' AND state='pending';

-- name: RuleMemberPrepareTask :one
SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND node_id=$3 AND phase='prepare';

-- name: RuleHasReleasedPort :one
SELECT EXISTS(SELECT 1 FROM port_allocations WHERE tenant_id=$1 AND rule_id=$2 AND state='released');

-- name: LockPortAllocation :one
SELECT * FROM port_allocations WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: LockPortGroup :one
SELECT * FROM node_groups WHERE id=$1 FOR UPDATE;

-- name: InsertPortReservation :exec
INSERT INTO port_reservations(tenant_id,allocation_id,node_id,protocol,port,listen_address,boot_id,generation,prepare_task_id,state)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'prepared');

-- name: PortReservations :many
SELECT * FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 ORDER BY node_id,protocol;

-- name: ConflictingPorts :many
SELECT protocol,port,listen_address FROM port_reservations WHERE node_id=$1 AND state<>'released' AND
(listen_address=$2 OR listen_address IN ('*','0.0.0.0','::') OR $2 IN ('*','0.0.0.0','::')) ORDER BY port,protocol;

-- name: CommitPortAllocation :one
UPDATE port_allocations SET state='committed',version=version+1 WHERE tenant_id=$1 AND id=$2 AND state='preparing' AND needs_revalidation=false RETURNING *;

-- name: SetPortAllocationState :exec
UPDATE port_allocations SET state=$3,version=version+1 WHERE tenant_id=$1 AND id=$2;

-- name: MarkGroupPortRevalidation :exec
UPDATE port_allocations SET needs_revalidation=true,version=version+1 WHERE group_id=$1 AND state NOT IN ('released','aborted');

-- name: RetainPortAllocation :exec
UPDATE port_allocations SET state='retained',first_effective_at=LEAST(first_effective_at,sqlc.arg(effective_at)::timestamptz),
retained_until=LEAST(first_effective_at,sqlc.arg(effective_at)::timestamptz)+interval '7 days',version=version+1 WHERE tenant_id=$1 AND id=$2;

-- name: ResetPortRetention :exec
UPDATE port_allocations SET state='preparing',first_effective_at=NULL,retained_until=NULL,version=version+1 WHERE tenant_id=$1 AND id=$2 AND state='retained';

-- name: ListTenantPortAllocations :many
SELECT * FROM port_allocations WHERE tenant_id=$1 AND state NOT IN ('released','aborted') ORDER BY group_id,id;

-- name: ClearPortRetentionClock :exec
UPDATE port_allocations SET first_effective_at=NULL,retained_until=NULL,version=version+1 WHERE tenant_id=$1 AND id=$2 AND state='retained';

-- name: RenewPortPrepareTask :exec
UPDATE port_reservations SET state='prepared',config_confirmed=false,prepare_task_id=$4,release_task_id=NULL WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND state='retained';

-- name: SetReservationState :exec
UPDATE port_reservations SET state=$3 WHERE tenant_id=$1 AND allocation_id=$2 AND state<>'released';

-- name: SetPortReleaseTask :exec
UPDATE port_reservations SET state='releasing',release_task_id=$4 WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND state<>'released';

-- name: ConfirmNodePortPrepare :exec
UPDATE port_reservations SET config_confirmed=true,state='confirmed' WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND state IN ('prepared','unknown');

-- name: MarkNodePortUnknown :exec
UPDATE port_reservations SET state='unknown' WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND state<>'released';

-- name: ConfirmNodePortRelease :exec
UPDATE port_reservations SET state='released' WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3;

-- name: GetPortProof :one
SELECT * FROM release_proofs WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND task_id=$4;

-- name: InsertPortProof :exec
INSERT INTO release_proofs(tenant_id,allocation_id,node_id,task_id,boot_id,revision,generation,proof_kind,receipt_sha256,peer_fingerprint)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: LockPortTask :one
SELECT t.*,o.resource_id,o.target_revision,o.generation FROM tasks t JOIN operations o ON o.id=t.operation_id AND o.tenant_id=t.tenant_id
WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t,o;

-- name: FinishPortTask :exec
UPDATE tasks SET state='succeeded',lease_until=NULL WHERE tenant_id=$1 AND id=$2;

-- name: PortServingMembers :many
SELECT DISTINCT n.id,n.public_address FROM port_allocations a JOIN port_reservations p ON p.tenant_id=a.tenant_id AND p.allocation_id=a.id
JOIN node_group_members m ON m.group_id=a.group_id AND m.node_id=p.node_id JOIN nodes n ON n.id=m.node_id
WHERE (a.tenant_id=sqlc.arg(tenant_id) OR sqlc.arg(platform_read)::boolean) AND a.group_id=sqlc.arg(group_id) AND a.member_set_version=sqlc.arg(member_set_version) AND a.state='committed' AND a.needs_revalidation=false
AND EXISTS(SELECT 1 FROM tenants t WHERE t.id=a.tenant_id AND t.account_state='active')
AND NOT EXISTS(SELECT 1 FROM current_restrictions cr WHERE cr.tenant_id=a.tenant_id AND (cr.resource_id IS NULL OR cr.resource_id=a.rule_id) AND cr.effective_at<=now() AND cr.cleared_at IS NULL)
AND p.state='confirmed' AND p.config_confirmed AND n.lifecycle='online' AND n.health<>'unhealthy' AND n.deleted_at IS NULL AND n.public_address<>'' AND n.boot_id=p.boot_id AND n.generation=p.generation
AND NOT EXISTS(SELECT 1 FROM port_reservations other WHERE other.tenant_id=a.tenant_id AND other.allocation_id=a.id AND other.node_id=n.id AND (NOT other.config_confirmed OR other.state<>'confirmed'))
AND NOT EXISTS(SELECT 1 FROM port_allocations required
 WHERE (required.tenant_id=a.tenant_id OR sqlc.arg(platform_read)::boolean) AND required.group_id=a.group_id AND required.state='committed'
 AND EXISTS(SELECT 1 FROM tenants t WHERE t.id=required.tenant_id AND t.account_state='active')
 AND NOT EXISTS(SELECT 1 FROM current_restrictions cr WHERE cr.tenant_id=required.tenant_id AND (cr.resource_id IS NULL OR cr.resource_id=required.rule_id) AND cr.effective_at<=now() AND cr.cleared_at IS NULL)
 AND (required.needs_revalidation OR required.member_set_version<>a.member_set_version
 OR (SELECT count(*) FROM port_reservations held WHERE held.tenant_id=required.tenant_id AND held.allocation_id=required.id AND held.node_id=n.id
 AND held.state='confirmed' AND held.config_confirmed AND held.boot_id=n.boot_id AND held.generation=n.generation)<>cardinality(required.protocols)))
ORDER BY n.id;

-- name: CountNodePortOccupation :one
SELECT count(*) FROM port_reservations WHERE node_id=$1 AND state<>'released';

-- name: RegisterNodeIncarnation :one
UPDATE nodes SET boot_id=$2,generation=$3,version=version+1 WHERE id=$1 AND generation<=$3 AND (boot_id='' OR boot_id=$2 OR generation<$3) RETURNING *;

-- name: LeasePortRelease :one
UPDATE tasks SET state='leased',lease_token=sqlc.arg(lease_token),lease_until=now()+interval '10 seconds',attempts=attempts+1
WHERE id=(SELECT t.id FROM tasks t JOIN outbox o ON o.tenant_id=t.tenant_id AND o.operation_id=t.operation_id
WHERE t.task_kind='port_release' AND t.state IN ('pending','unknown')
AND o.delivered_at IS NULL ORDER BY t.attempts,o.id FOR UPDATE OF t SKIP LOCKED LIMIT 1) RETURNING *;

-- name: PortReleaseBinding :one
SELECT p.allocation_id,p.node_id,p.boot_id,p.generation,p.prepare_task_id,d.created_by
FROM port_reservations p JOIN rule_delivery_bindings b ON b.tenant_id=p.tenant_id AND b.task_id=p.prepare_task_id
JOIN rule_deployments d ON d.tenant_id=b.tenant_id AND d.id=b.deployment_id
WHERE p.tenant_id=$1 AND p.release_task_id=$2 ORDER BY p.protocol LIMIT 1;

-- name: ExpirePortReleaseLeases :exec
UPDATE tasks SET state='unknown',lease_until=NULL WHERE task_kind='port_release' AND state IN ('leased','running') AND lease_until<now();
