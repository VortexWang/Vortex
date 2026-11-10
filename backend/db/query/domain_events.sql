-- name: DomainEventBounds :one
SELECT coalesce((SELECT last_sequence FROM domain_event_streams s WHERE s.tenant_id=sqlc.arg(tenant_id)),0)::bigint AS last_sequence,
 coalesce((SELECT retained_after FROM domain_event_streams s WHERE s.tenant_id=sqlc.arg(tenant_id)),0)::bigint AS retained_after;

-- name: ReplayDomainEvents :many
SELECT sequence,event_type,aggregate_id,revision,summary,created_at FROM domain_events
WHERE tenant_id=sqlc.arg(tenant_id) AND sequence>sqlc.arg(after_sequence)
ORDER BY sequence LIMIT sqlc.arg(page_limit)::integer;

-- name: LockDomainEventStream :one
SELECT * FROM domain_event_streams WHERE tenant_id=$1 FOR UPDATE;

-- name: RetentionEventFloor :one
SELECT coalesce((SELECT min(sequence)-1 FROM domain_events WHERE tenant_id=sqlc.arg(tenant_id) AND created_at>=sqlc.arg(cutoff)::timestamptz),sqlc.arg(last_sequence))::bigint AS floor;

-- name: AdvanceEventRetention :exec
UPDATE domain_event_streams SET retained_after=greatest(retained_after,sqlc.arg(retained_after)) WHERE tenant_id=sqlc.arg(tenant_id);

-- name: DeleteRetainedDomainEvents :execrows
DELETE FROM domain_events e WHERE e.tenant_id=$1 AND e.sequence<=(SELECT retained_after FROM domain_event_streams s WHERE s.tenant_id=$1);

-- name: ReadDomainEventPage :one
SELECT coalesce((SELECT s.last_sequence FROM domain_event_streams s WHERE s.tenant_id=sqlc.arg(tenant_id)),0)::bigint AS last_sequence,
 coalesce((SELECT s.retained_after FROM domain_event_streams s WHERE s.tenant_id=sqlc.arg(tenant_id)),0)::bigint AS retained_after,
 coalesce((SELECT jsonb_agg(jsonb_build_object('sequence',p.sequence,'type',p.event_type,'resource_id',p.aggregate_id,'revision',p.revision,'summary',p.summary) ORDER BY p.sequence)
 FROM (SELECT e.sequence,e.event_type,e.aggregate_id,e.revision,e.summary FROM domain_events e
 WHERE e.tenant_id=sqlc.arg(tenant_id) AND e.sequence>sqlc.arg(after_sequence)
 ORDER BY e.sequence LIMIT sqlc.arg(page_limit)::integer) p),'[]'::jsonb)::jsonb AS events;

-- name: DomainEventRetentionCandidates :many
SELECT s.tenant_id FROM domain_event_streams s
WHERE EXISTS(SELECT 1 FROM domain_events e WHERE e.tenant_id=s.tenant_id AND e.created_at<sqlc.arg(cutoff)::timestamptz)
ORDER BY s.tenant_id LIMIT 32;
