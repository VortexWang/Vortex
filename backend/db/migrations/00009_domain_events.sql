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
-- Domain event rows are safe business summaries, never runtime payloads or secrets.
CREATE TABLE domain_events (
 id bigserial PRIMARY KEY,
 tenant_id text NOT NULL REFERENCES tenants(id),
 event_type text NOT NULL,
 aggregate_id text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 summary jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX domain_events_scope_cursor ON domain_events(tenant_id,id);
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
-- +goose Down
DROP TRIGGER operation_public_order ON operations;
DROP FUNCTION guard_operation_public_order();
DROP INDEX operations_public_cursor;
ALTER TABLE operations DROP COLUMN public_created_at;
DROP TABLE domain_events, refresh_token_revocations, api_access_tokens, refresh_tokens, refresh_token_families;
