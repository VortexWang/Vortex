-- +goose Up
CREATE TABLE refresh_token_families (
 id text PRIMARY KEY,
 actor_id text NOT NULL,
 subject_kind text NOT NULL CHECK(subject_kind IN ('tenant','platform')),
 tenant_id text,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 CHECK ((subject_kind='tenant' AND tenant_id IS NOT NULL) OR (subject_kind='platform' AND tenant_id IS NULL))
);
CREATE TABLE refresh_tokens (
 token_sha256 bytea PRIMARY KEY CHECK(octet_length(token_sha256)=32),
 family_id text NOT NULL REFERENCES refresh_token_families(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 consumed_at timestamptz,
 expires_at timestamptz NOT NULL
);
CREATE INDEX refresh_tokens_family ON refresh_tokens(family_id);
CREATE TABLE api_access_tokens (
 token_sha256 bytea PRIMARY KEY CHECK(octet_length(token_sha256)=32),
 family_id text NOT NULL REFERENCES refresh_token_families(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX api_access_tokens_family ON api_access_tokens(family_id);
CREATE TABLE refresh_token_revocations (
 family_id text PRIMARY KEY REFERENCES refresh_token_families(id),
 reason text NOT NULL CHECK(reason IN ('logout','reuse')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER token_revocations_immutable BEFORE UPDATE OR DELETE ON refresh_token_revocations
FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Public sequence allocation happens at deferred commit, after business locks.
-- A global bigserial alone cannot safely represent concurrent commit order.
CREATE TABLE domain_event_streams (
 tenant_id text PRIMARY KEY REFERENCES tenants(id),
 last_sequence bigint NOT NULL DEFAULT 0 CHECK(last_sequence>=0),
 retained_after bigint NOT NULL DEFAULT 0 CHECK(retained_after>=0 AND retained_after<=last_sequence)
);
CREATE TABLE domain_events (
 id bigserial PRIMARY KEY,
 tenant_id text NOT NULL REFERENCES tenants(id),
 sequence bigint NOT NULL CHECK(sequence>0),
 event_type text NOT NULL CHECK(event_type IN ('operation_changed','restriction_changed')),
 aggregate_id text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 summary jsonb NOT NULL CHECK(jsonb_typeof(summary)='object' AND octet_length(summary::text)<=4096),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,sequence)
);
CREATE INDEX domain_events_retention ON domain_events(tenant_id,created_at,sequence);
-- Transactional safe summaries waiting for deferred sequence assignment. No
-- committed pending row remains after a successful flush; no in-memory relay.
CREATE TABLE domain_event_pending (
 id bigserial PRIMARY KEY,
 transaction_id bigint NOT NULL DEFAULT txid_current(),
 tenant_id text NOT NULL REFERENCES tenants(id),
 event_type text NOT NULL,
 aggregate_id text NOT NULL,
 revision bigint NOT NULL,
 summary jsonb NOT NULL
);
CREATE INDEX domain_pending_transaction ON domain_event_pending(transaction_id,id);
-- Public ordering is independent of mutable worker scheduling timestamps.
ALTER TABLE operations ADD COLUMN public_created_at timestamptz;
UPDATE operations SET public_created_at=created_at;
ALTER TABLE operations ALTER COLUMN public_created_at SET NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION guard_operation_public_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  NEW.public_created_at := NEW.created_at;
 ELSIF NEW.public_created_at IS DISTINCT FROM OLD.public_created_at
    OR NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
  RAISE EXCEPTION 'immutable operation public order';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER operation_public_order BEFORE INSERT OR UPDATE ON operations
FOR EACH ROW EXECUTE FUNCTION guard_operation_public_order();
CREATE INDEX operations_public_cursor ON operations(tenant_id,public_created_at,id);
-- +goose StatementBegin
CREATE FUNCTION queue_operation_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.state=OLD.state AND NEW.occupation_held=OLD.occupation_held AND NEW.target_revision=OLD.target_revision THEN RETURN NEW; END IF;
 INSERT INTO domain_event_pending(tenant_id,event_type,aggregate_id,revision,summary)
 VALUES(NEW.tenant_id,'operation_changed',NEW.id,NEW.target_revision,
 jsonb_build_object('operation_id',NEW.id,'resource_id',NEW.resource_id,'state',NEW.state,'occupation_held',NEW.occupation_held));
 RETURN NEW;
END $$;
CREATE FUNCTION queue_restriction_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO domain_event_pending(tenant_id,event_type,aggregate_id,revision,summary)
 VALUES(NEW.tenant_id,'restriction_changed',coalesce(NEW.resource_id,NEW.tenant_id),NEW.source_version,
 jsonb_build_object('cause',NEW.cause,'event_kind',NEW.event_kind,'effective_at',NEW.effective_at));
 RETURN NEW;
END $$;
CREATE FUNCTION flush_domain_events() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target text; pending domain_event_pending; next_sequence bigint;
BEGIN
 -- All affected streams lock in one order, after all originating business
 -- writes. This avoids row->stream->row and cross-tenant commit lock cycles.
 FOR target IN SELECT DISTINCT tenant_id FROM domain_event_pending WHERE transaction_id=txid_current() ORDER BY tenant_id LOOP
  INSERT INTO domain_event_streams(tenant_id) VALUES(target) ON CONFLICT DO NOTHING;
  PERFORM 1 FROM domain_event_streams WHERE tenant_id=target FOR UPDATE;
 END LOOP;
 FOR pending IN SELECT * FROM domain_event_pending WHERE transaction_id=txid_current() ORDER BY id LOOP
  UPDATE domain_event_streams SET last_sequence=last_sequence+1 WHERE tenant_id=pending.tenant_id RETURNING last_sequence INTO next_sequence;
  INSERT INTO domain_events(tenant_id,sequence,event_type,aggregate_id,revision,summary)
  VALUES(pending.tenant_id,next_sequence,pending.event_type,pending.aggregate_id,pending.revision,pending.summary);
 END LOOP;
 DELETE FROM domain_event_pending WHERE transaction_id=txid_current();
 RETURN NULL;
END $$;
CREATE FUNCTION guard_domain_event_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND OLD.sequence<=(SELECT retained_after FROM domain_event_streams WHERE tenant_id=OLD.tenant_id) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'immutable domain event history';
END $$;
CREATE FUNCTION guard_domain_stream() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.tenant_id<>OLD.tenant_id OR NEW.last_sequence<OLD.last_sequence OR NEW.retained_after<OLD.retained_after THEN RAISE EXCEPTION 'monotonic domain event stream'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER operation_domain_event AFTER INSERT OR UPDATE ON operations FOR EACH ROW EXECUTE FUNCTION queue_operation_event();
CREATE TRIGGER restriction_domain_event AFTER INSERT ON restriction_events FOR EACH ROW EXECUTE FUNCTION queue_restriction_event();
CREATE CONSTRAINT TRIGGER flush_committed_domain_events AFTER INSERT ON domain_event_pending DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION flush_domain_events();
CREATE TRIGGER domain_event_immutable BEFORE UPDATE OR DELETE ON domain_events FOR EACH ROW EXECUTE FUNCTION guard_domain_event_history();
CREATE TRIGGER domain_stream_monotonic BEFORE UPDATE OR DELETE ON domain_event_streams FOR EACH ROW EXECUTE FUNCTION guard_domain_stream();
-- Public ordering is a separate immutable registry so mutable business heads
-- and historical generated models keep their existing columns. Backfilled rows
-- share the migration timestamp; their stable ID breaks ties. This is ordering,
-- never ownership or authorization, and does not claim a historical creation date.
CREATE TABLE public_resource_order (
 tenant_key text NOT NULL, collection text NOT NULL, resource_key text NOT NULL,
 sort_id text NOT NULL CHECK(length(sort_id) BETWEEN 1 AND 128),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_key,collection,resource_key), UNIQUE(tenant_key,collection,sort_id)
);
CREATE INDEX public_resource_cursor ON public_resource_order(collection,tenant_key,created_at,sort_id);
CREATE TRIGGER public_resource_order_immutable BEFORE UPDATE OR DELETE ON public_resource_order FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- +goose StatementBegin
CREATE FUNCTION register_public_resource_order() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE value jsonb:=to_jsonb(NEW); prior jsonb; tenant text:=''; resource text; sort_value text;
BEGIN
 IF TG_ARGV[2]<>'' THEN tenant:=value->>TG_ARGV[2]; END IF;
 IF TG_ARGV[1]='restriction' THEN resource:=jsonb_build_array(value->>'cause',value->>'source_id')::text;
 ELSE resource:=value->>TG_ARGV[1]; END IF;
 IF TG_OP='UPDATE' THEN
  prior:=to_jsonb(OLD);
  IF (TG_ARGV[2]<>'' AND value->>TG_ARGV[2] IS DISTINCT FROM prior->>TG_ARGV[2]) OR
   (TG_ARGV[1]='restriction' AND (value->>'cause' IS DISTINCT FROM prior->>'cause' OR value->>'source_id' IS DISTINCT FROM prior->>'source_id')) OR
   (TG_ARGV[1]<>'restriction' AND value->>TG_ARGV[1] IS DISTINCT FROM prior->>TG_ARGV[1]) THEN
   RAISE EXCEPTION 'immutable public resource identity';
  END IF;
  RETURN NEW;
 END IF;
 sort_value:=CASE WHEN TG_ARGV[1]='restriction' THEN gen_random_uuid()::text ELSE resource END;
 INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) VALUES(tenant,TG_ARGV[0],resource,sort_value) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT id,'tenants',id,id FROM tenants;
CREATE TRIGGER tenants_public_order BEFORE INSERT OR UPDATE ON tenants FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('tenants','id','id');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT '','nodes',id,id FROM nodes;
CREATE TRIGGER nodes_public_order BEFORE INSERT OR UPDATE ON nodes FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('nodes','id','');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT '','node-groups',id,id FROM node_groups;
CREATE TRIGGER node_groups_public_order BEFORE INSERT OR UPDATE ON node_groups FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('node-groups','id','');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT tenant_id,'rules',id,id FROM rule_heads;
CREATE TRIGGER rule_heads_public_order BEFORE INSERT OR UPDATE ON rule_heads FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('rules','id','tenant_id');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT '','packages',id,id FROM package_catalog;
CREATE TRIGGER package_catalog_public_order BEFORE INSERT OR UPDATE ON package_catalog FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('packages','id','');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT tenant_id,'subscriptions',id,id FROM subscriptions;
CREATE TRIGGER subscriptions_public_order BEFORE INSERT OR UPDATE ON subscriptions FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('subscriptions','id','tenant_id');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT tenant_id,'port-allocations',id,id FROM port_allocations;
CREATE TRIGGER port_allocations_public_order BEFORE INSERT OR UPDATE ON port_allocations FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('port-allocations','id','tenant_id');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT tenant_id,'sources',source_id,source_id FROM ledger_projections;
CREATE TRIGGER ledger_projections_public_order BEFORE INSERT OR UPDATE ON ledger_projections FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('sources','source_id','tenant_id');
INSERT INTO public_resource_order(tenant_key,collection,resource_key,sort_id) SELECT tenant_id,'restrictions',jsonb_build_array(cause,source_id)::text,gen_random_uuid()::text FROM current_restrictions;
CREATE TRIGGER current_restrictions_public_order BEFORE INSERT OR UPDATE ON current_restrictions FOR EACH ROW EXECUTE FUNCTION register_public_resource_order('restrictions','restriction','tenant_id');
-- Aggregate port operations bind one accepted allocation intent to the exact
-- frozen reservation count; they never substitute for authenticated receipts.
CREATE TABLE port_operation_bindings (
 tenant_id text NOT NULL, allocation_id text NOT NULL, operation_id text NOT NULL,
 action text NOT NULL CHECK(action='prepare'), allocation_version bigint NOT NULL CHECK(allocation_version>0),
 actor_id text NOT NULL, expected_reservations integer NOT NULL CHECK(expected_reservations BETWEEN 1 AND 2048),
 PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,allocation_id,action,allocation_version),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES port_allocations(tenant_id,id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TRIGGER port_operation_binding_immutable BEFORE UPDATE OR DELETE ON port_operation_bindings
 FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE VIEW port_operation_projection AS
WITH evidence AS (
 SELECT b.*,a.state AS allocation_state,a.needs_revalidation,o.state AS actual_state,o.occupation_held AS actual_held,
 count(p.node_id) AS actual_reservations,
 COALESCE(bool_and(p.config_confirmed AND p.state='confirmed' AND pp.task_id IS NOT NULL),false) AS all_prepared,
 COALESCE(bool_or(p.state='unknown' OR pt.state='unknown'),false) AS has_unknown,
 COALESCE(bool_and(p.state='released' AND rp.task_id IS NOT NULL),false) AS all_released
 FROM port_operation_bindings b
 JOIN port_allocations a ON a.tenant_id=b.tenant_id AND a.id=b.allocation_id
 JOIN operations o ON o.tenant_id=b.tenant_id AND o.id=b.operation_id
 LEFT JOIN port_reservations p ON p.tenant_id=b.tenant_id AND p.allocation_id=b.allocation_id
 LEFT JOIN tasks pt ON pt.tenant_id=p.tenant_id AND pt.id=p.prepare_task_id
 LEFT JOIN operations po ON po.tenant_id=pt.tenant_id AND po.id=pt.operation_id
 LEFT JOIN release_proofs pp ON pp.tenant_id=p.tenant_id AND pp.allocation_id=p.allocation_id
  AND pp.node_id=p.node_id AND pp.task_id=p.prepare_task_id AND pp.proof_kind='prepare'
  AND pp.boot_id=p.boot_id AND pp.generation=p.generation AND pp.revision=po.target_revision
 LEFT JOIN tasks rt ON rt.tenant_id=p.tenant_id AND rt.id=p.release_task_id
 LEFT JOIN operations ro ON ro.tenant_id=rt.tenant_id AND ro.id=rt.operation_id
 LEFT JOIN release_proofs rp ON rp.tenant_id=p.tenant_id AND rp.allocation_id=p.allocation_id
  AND rp.node_id=p.node_id AND rp.task_id=p.release_task_id AND rp.proof_kind='release'
  AND rp.boot_id=p.boot_id AND rp.generation=p.generation AND rp.revision=ro.target_revision
 GROUP BY b.tenant_id,b.operation_id,b.allocation_id,b.action,b.allocation_version,b.actor_id,b.expected_reservations,
 a.state,a.needs_revalidation,o.state,o.occupation_held
)
SELECT *,CASE
 WHEN actual_state IN ('succeeded','failed') THEN actual_state
 WHEN needs_revalidation OR allocation_state IN ('aborting','releasing','released','aborted') THEN 'failed'
 WHEN actual_reservations<>expected_reservations OR has_unknown THEN 'unknown'
 WHEN all_prepared THEN 'succeeded' ELSE 'pending' END::text AS desired_state,
 NOT(allocation_state IN ('released','aborted') AND actual_reservations=expected_reservations AND all_released)::boolean AS desired_held
FROM evidence;
-- +goose Down
DROP VIEW port_operation_projection;
DROP TABLE port_operation_bindings;
DROP TRIGGER tenants_public_order ON tenants;
DROP TRIGGER nodes_public_order ON nodes;
DROP TRIGGER node_groups_public_order ON node_groups;
DROP TRIGGER rule_heads_public_order ON rule_heads;
DROP TRIGGER package_catalog_public_order ON package_catalog;
DROP TRIGGER subscriptions_public_order ON subscriptions;
DROP TRIGGER port_allocations_public_order ON port_allocations;
DROP TRIGGER ledger_projections_public_order ON ledger_projections;
DROP TRIGGER current_restrictions_public_order ON current_restrictions;
DROP FUNCTION register_public_resource_order();
DROP TABLE public_resource_order;

DROP TRIGGER operation_domain_event ON operations;
DROP TRIGGER restriction_domain_event ON restriction_events;
DROP TABLE domain_event_pending;
DROP FUNCTION flush_domain_events(),queue_operation_event(),queue_restriction_event();
DROP TABLE domain_events,domain_event_streams;
DROP FUNCTION guard_domain_event_history(),guard_domain_stream();
DROP TRIGGER operation_public_order ON operations;
DROP FUNCTION guard_operation_public_order();
DROP INDEX operations_public_cursor;
ALTER TABLE operations DROP COLUMN public_created_at;
DROP TABLE refresh_token_revocations, api_access_tokens, refresh_tokens, refresh_token_families;
