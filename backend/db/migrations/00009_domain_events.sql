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
-- +goose Down
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
