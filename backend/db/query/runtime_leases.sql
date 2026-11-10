-- name: GetHubRuntimeLease :one
SELECT * FROM hub_runtime_lease_receipts WHERE tenant_id=$1 AND issue_id=$2;
-- name: AppendHubRuntimeLease :exec
INSERT INTO hub_runtime_lease_receipts(tenant_id,issue_id,allocation_id,hub_id,hub_boot_id,ledger_sequence,payload,payload_digest,peer_fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: LatestHubRuntimeAuthorityTime :one
SELECT coalesce(max((convert_from(payload,'UTF8')::jsonb->>'issued_at')::timestamptz),'epoch'::timestamptz)::timestamptz AS issued_at FROM (
SELECT i.payload FROM hub_issue_receipts i WHERE i.tenant_id=$1 AND i.allocation_id=$2
UNION ALL SELECT l.payload FROM hub_runtime_lease_receipts l WHERE l.tenant_id=$1 AND l.allocation_id=$2 AND jsonb_array_length(convert_from(l.payload,'UTF8')::jsonb->'authorizations')=2
) history;
-- name: HubAllocationByReservation :one
SELECT * FROM hub_funded_allocations WHERE reservation_id=$1;
-- name: LatestHubRuntimeHeartbeatTime :one
SELECT coalesce(max((convert_from(l.payload,'UTF8')::jsonb->>'issued_at')::timestamptz),'epoch'::timestamptz)::timestamptz AS issued_at
FROM (
SELECT i.payload FROM hub_issue_receipts i WHERE i.tenant_id=$1 AND i.allocation_id=$2
UNION ALL SELECT h.payload FROM hub_runtime_lease_receipts h WHERE h.tenant_id=$1 AND h.allocation_id=$2 AND jsonb_array_length(convert_from(h.payload,'UTF8')::jsonb->'authorizations')=0
) l;
