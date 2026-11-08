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
-- +goose Down
DROP TABLE domain_events, refresh_token_revocations, api_access_tokens, refresh_tokens, refresh_token_families;
