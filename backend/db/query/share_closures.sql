-- name: ShareClosureCandidates :many
SELECT DISTINCT h.* FROM share_handoffs h
JOIN share_generations g ON g.tenant_id=h.tenant_id AND g.node_id=h.node_id AND g.kind=h.kind AND g.generation=h.generation
JOIN share_renewal_authorities a ON a.tenant_id=g.tenant_id AND a.node_id=g.node_id AND a.kind=g.kind AND a.generation=g.predecessor
WHERE h.command IS NULL AND h.state='prepared' AND a.closed_at IS NULL
AND NOT EXISTS(SELECT 1 FROM share_renewal_closure_commands c WHERE c.tenant_id=h.tenant_id AND c.handoff_id=h.id AND c.signer_id=a.signer_id)
ORDER BY h.tenant_id,h.id LIMIT 100;
-- name: ShareClosureDelegations :many
SELECT a.delegation_id FROM hub_funded_allocations a
JOIN hub_allocation_share_bindings b ON b.tenant_id=a.tenant_id AND b.allocation_id=a.allocation_id AND b.node_id=a.node_id
JOIN share_generations g ON g.tenant_id=b.tenant_id AND g.node_id=b.node_id AND g.kind=b.kind AND g.generation=b.generation AND g.boot_id=a.boot_id AND g.signer_id=a.hub_id
WHERE a.tenant_id=$1 AND a.node_id=$2 AND a.boot_id=$3 AND a.hub_id=$4 AND a.hub_boot_id=$5 AND b.kind=$6 AND b.generation=$7 AND g.signer_epoch=$8 ORDER BY a.delegation_id;
-- name: UnboundShareClosureDelegations :one
SELECT count(*) FROM hub_funded_allocations a
WHERE a.tenant_id=$1 AND a.node_id=$2 AND a.boot_id=$3 AND a.hub_id=$4 AND a.hub_boot_id=$5
AND (SELECT count(*) FROM hub_allocation_share_bindings b
JOIN share_generations g ON g.tenant_id=b.tenant_id AND g.node_id=b.node_id AND g.kind=b.kind AND g.generation=b.generation AND g.boot_id=a.boot_id AND g.signer_id=a.hub_id
WHERE b.tenant_id=a.tenant_id AND b.allocation_id=a.allocation_id AND b.node_id=a.node_id)<>2;
-- name: ShareClosureCommands :many
SELECT * FROM share_renewal_closure_commands WHERE tenant_id=$1 AND handoff_id=$2 AND signer_id=$3 ORDER BY part;
-- name: AppendShareClosureCommand :exec
INSERT INTO share_renewal_closure_commands(tenant_id,id,handoff_id,node_id,boot_id,kind,generation,signer_id,signer_epoch,hub_boot_id,part,parts,operation_id,payload,payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15);
-- name: ShareClosureBarrier :one
SELECT EXISTS(SELECT 1 FROM share_renewal_closure_commands WHERE tenant_id=$1 AND node_id=$2 AND boot_id=$3 AND kind=$4 AND generation=$5 AND signer_id=$6);
-- name: PendingShareClosures :many
SELECT c.* FROM share_renewal_closure_commands c WHERE c.signer_id=$1 AND c.hub_boot_id=$2 AND NOT EXISTS(SELECT 1 FROM share_renewal_closure_receipts r WHERE r.tenant_id=c.tenant_id AND r.command_id=c.id) ORDER BY c.tenant_id,c.handoff_id,c.part LIMIT 1;
-- name: GetShareClosureCommand :one
SELECT * FROM share_renewal_closure_commands WHERE id=$1;
-- name: GetShareClosureReceipt :one
SELECT * FROM share_renewal_closure_receipts WHERE tenant_id=$1 AND command_id=$2;
-- name: AppendShareClosureReceipt :exec
INSERT INTO share_renewal_closure_receipts(tenant_id,command_id,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5);
-- name: RemainingShareClosures :one
SELECT count(*) FROM share_renewal_closure_commands c WHERE c.tenant_id=$1 AND c.handoff_id=$2 AND c.signer_id=$3 AND NOT EXISTS(SELECT 1 FROM share_renewal_closure_receipts r WHERE r.tenant_id=c.tenant_id AND r.command_id=c.id);
