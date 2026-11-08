-- Public reads deliberately select safe business fields instead of serializing
-- generated database models (which can contain credentials and runtime data).
-- name: GetPublicTenant :one
SELECT jsonb_build_object('id',id,'account_state',account_state,'version',version) AS body
FROM tenants WHERE id=sqlc.arg(tenant_id);

-- name: GetPublicNode :one
SELECT jsonb_build_object('id',n.id,'name',n.name,'region',n.region,'capabilities',n.capabilities,'health',n.health,'lifecycle',n.lifecycle,'version',n.version) AS body
FROM nodes n JOIN resource_ownership o ON o.resource_id=n.id
WHERE n.id=sqlc.arg(resource_id) AND n.deleted_at IS NULL AND
(o.owner_tenant_id=sqlc.arg(tenant_id) OR (o.owner_kind='platform' AND EXISTS(
 SELECT 1 FROM resource_grants g WHERE g.resource_id=n.id AND g.tenant_id=sqlc.arg(tenant_id)
 AND g.status='active' AND g.valid_from<=now() AND g.valid_until>now())));

-- name: GetPublicNodeGroup :one
SELECT jsonb_build_object('id',n.id,'name',n.name,'purpose',n.purpose,'scheduling',n.scheduling,'billing_multiplier_milli',n.billing_multiplier_milli,'billing_version',n.billing_version,'member_version',n.member_version,'version',n.version) AS body
FROM node_groups n JOIN resource_ownership o ON o.resource_id=n.id
WHERE n.id=sqlc.arg(resource_id) AND n.deleted_at IS NULL AND
(o.owner_tenant_id=sqlc.arg(tenant_id) OR (o.owner_kind='platform' AND EXISTS(
 SELECT 1 FROM resource_grants g WHERE g.resource_id=n.id AND g.tenant_id=sqlc.arg(tenant_id)
 AND g.status='active' AND g.valid_from<=now() AND g.valid_until>now())));

-- name: GetPublicSubscription :one
SELECT jsonb_build_object('id',s.id,'snapshot_id',s.snapshot_id,'version',s.version,'anchor_at',s.anchor_at,'expires_at',s.expires_at,'validity_state',CASE WHEN s.expires_at>now() THEN 'active' ELSE 'expired' END,'terms',p.terms) AS body
FROM subscriptions s JOIN package_snapshots p ON p.tenant_id=s.tenant_id AND p.id=s.snapshot_id
WHERE s.tenant_id=sqlc.arg(tenant_id);

-- name: GetPublicPackage :one
SELECT jsonb_build_object('id',c.id,'name',c.name,'version',c.current_version,'terms',p.terms) AS body
FROM package_catalog c JOIN package_versions p ON p.package_id=c.id AND p.version=c.current_version
WHERE c.id=sqlc.arg(resource_id) AND c.enabled;

-- name: GetPublicRule :one
SELECT jsonb_build_object('id',h.id,'desired_revision',h.desired_revision,'active_revision',h.active_revision,'sync_status',h.sync_status,'name',convert_from(r.payload,'UTF8')::jsonb->'name','protocol',convert_from(r.payload,'UTF8')::jsonb->'protocol','mode',convert_from(r.payload,'UTF8')::jsonb->'mode','enabled',convert_from(r.payload,'UTF8')::jsonb->'enabled') AS body
FROM rule_heads h JOIN rule_revisions r ON r.tenant_id=h.tenant_id AND r.rule_id=h.id AND r.revision=h.desired_revision
WHERE h.tenant_id=sqlc.arg(tenant_id) AND h.id=sqlc.arg(resource_id);

-- name: GetPublicSourceLedger :one
SELECT jsonb_build_object('source_id',source_id,'balance_bytes',b::text,'outstanding_bytes',f::text,'guarantee_bytes',l::text,'charged_bytes',charged::text,'remainder_milli',remainder_milli,'sequence',sequence::text) AS body
FROM ledger_projections WHERE tenant_id=sqlc.arg(tenant_id) AND source_id=sqlc.arg(resource_id);

-- name: GetPublicLimits :one
WITH current_handoffs AS (
 SELECT h.* FROM share_handoffs h JOIN share_chains c ON c.tenant_id=h.tenant_id AND c.node_id=h.node_id AND c.kind=h.kind AND c.head_generation=h.generation
 WHERE h.tenant_id=sqlc.arg(tenant_id) AND c.retired_generation<>h.generation
)
SELECT jsonb_build_object(
 'rate_limit_bps',coalesce((SELECT p.terms->>'rate_limit' FROM subscriptions s JOIN package_snapshots p ON p.tenant_id=s.tenant_id AND p.id=s.snapshot_id WHERE s.tenant_id=t.id),'0'),
 'connection_limit',coalesce((SELECT p.terms->>'connection_limit' FROM subscriptions s JOIN package_snapshots p ON p.tenant_id=s.tenant_id AND p.id=s.snapshot_id WHERE s.tenant_id=t.id),'0'),
 'reserved_rate_bps',(SELECT coalesce(sum(held_amount),0)::text FROM share_chains WHERE tenant_id=t.id AND kind='rate'),
 'reserved_connections',(SELECT coalesce(sum(held_amount),0)::text FROM share_chains WHERE tenant_id=t.id AND kind='connections'),
 'execution_state',CASE WHEN EXISTS(SELECT 1 FROM current_handoffs WHERE tenant_id=t.id AND execution_state='failed') THEN 'failed'
 WHEN EXISTS(SELECT 1 FROM current_handoffs WHERE tenant_id=t.id AND execution_state='enforcing') THEN 'enforcing'
 WHEN NOT EXISTS(SELECT 1 FROM current_handoffs WHERE tenant_id=t.id) OR EXISTS(SELECT 1 FROM current_handoffs WHERE tenant_id=t.id AND execution_state='submitted') THEN 'submitted' ELSE 'enforced' END,
 'operation_ids',coalesce((SELECT jsonb_agg(operation_id ORDER BY operation_id) FROM (SELECT operation_id FROM current_handoffs WHERE tenant_id=t.id AND execution_state<>'enforced' ORDER BY operation_id LIMIT 1001) bounded),'[]'::jsonb)) AS body
FROM tenants t WHERE t.id=sqlc.arg(tenant_id);
