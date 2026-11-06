-- Phase 6 implemented PostgreSQL schema through migration 00002.
-- Canonical executable source: backend/db/migrations/*.sql.
-- Future node/rule/ledger schemas are introduced by their ordered migrations.

CREATE TABLE desired_state (
    tenant_id text NOT NULL,
    resource_id text NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('node', 'hub')),
    revision bigint NOT NULL CHECK (revision > 0),
    payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 524288),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    PRIMARY KEY (tenant_id, resource_id)
);
CREATE TABLE operations (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    resource_id text NOT NULL,
    idempotency_key text NOT NULL,
    request_sha256 bytea NOT NULL CHECK (octet_length(request_sha256) = 32),
    target_kind text NOT NULL CHECK (target_kind IN ('node', 'hub')),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    expected_revision bigint NOT NULL CHECK (expected_revision >= 0),
    generation bigint NOT NULL CHECK (generation > 0),
    payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 524288),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','leased','running','succeeded','failed','unknown')),
    occupation_held boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,idempotency_key),
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,resource_id) REFERENCES desired_state(tenant_id,resource_id)
);
CREATE TABLE tasks (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    operation_id text NOT NULL UNIQUE,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','leased','running','succeeded','failed','unknown')),
    lease_token text,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    FOREIGN KEY (tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TABLE outbox (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL,
    operation_id text NOT NULL UNIQUE,
    delivered_at timestamptz,
    FOREIGN KEY (tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE INDEX tasks_pending ON tasks(state, lease_until);
CREATE INDEX outbox_pending ON outbox(id) WHERE delivered_at IS NULL;
CREATE TABLE audit_events (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL,
    resource_id text NOT NULL,
    request_id text NOT NULL,
    before_summary jsonb NOT NULL,
    after_summary jsonb NOT NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audit events are immutable'; END;
$$;
CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE control_receipts (
    tenant_id text NOT NULL,
    operation_id text NOT NULL,
    target_revision bigint NOT NULL,
    generation bigint NOT NULL,
    receipt_sha256 bytea NOT NULL CHECK (octet_length(receipt_sha256)=32),
    state text NOT NULL CHECK (state IN ('succeeded','failed','unknown')),
    PRIMARY KEY(tenant_id,operation_id),
    FOREIGN KEY (tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TABLE control_observations (
    hub_id text NOT NULL,
    session_id text NOT NULL,
    payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 1048576),
    observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(hub_id,session_id)
);

CREATE TABLE tenants (
    id text PRIMARY KEY CHECK (id <> ''),
    account_state text NOT NULL DEFAULT 'active' CHECK (account_state IN ('active','suspended','banned')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE users (
    id text PRIMARY KEY CHECK (id <> ''),
    tenant_id text NOT NULL REFERENCES tenants(id),
    role text NOT NULL CHECK (role IN ('owner','member')),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','suspended','banned')),
    UNIQUE (tenant_id,id)
);
CREATE TABLE platform_principals (
    id text PRIMARY KEY CHECK (id <> ''),
    role text NOT NULL CHECK (role IN ('platform_admin','operator','auditor')),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','suspended','banned'))
);
-- This registry expresses ownership; domain node/group tables are added later.
CREATE TABLE resource_ownership (
    resource_id text PRIMARY KEY,
    resource_kind text NOT NULL CHECK (resource_kind IN ('node','node_group')),
    owner_kind text NOT NULL CHECK (owner_kind IN ('platform','tenant')),
    owner_tenant_id text REFERENCES tenants(id),
    CHECK ((owner_kind='platform' AND owner_tenant_id IS NULL) OR (owner_kind='tenant' AND owner_tenant_id IS NOT NULL)),
    UNIQUE(resource_id,owner_kind)
);
CREATE TABLE resource_grants (
    id text PRIMARY KEY,
    tenant_id text NOT NULL REFERENCES tenants(id),
    resource_id text NOT NULL,
    owner_kind text NOT NULL DEFAULT 'platform' CHECK (owner_kind='platform'),
    version bigint NOT NULL CHECK (version > 0),
    status text NOT NULL CHECK (status IN ('active','revoked')),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NOT NULL CHECK (valid_until > valid_from),
    UNIQUE(tenant_id,resource_id,version),
    FOREIGN KEY(resource_id,owner_kind) REFERENCES resource_ownership(resource_id,owner_kind)
);
CREATE TABLE restriction_events (
    id text PRIMARY KEY,
    tenant_id text NOT NULL REFERENCES tenants(id),
    cause text NOT NULL CHECK (cause IN ('account_suspended','account_banned','package_expired','quota_exhausted','quantity_exceeded','manual')),
    source_id text NOT NULL CHECK (source_id <> ''),
    source_version bigint NOT NULL CHECK (source_version > 0),
    event_kind text NOT NULL CHECK (event_kind IN ('applied','cleared')),
    effective_at timestamptz NOT NULL,
    cleared_at timestamptz,
    CHECK ((event_kind='applied' AND cleared_at IS NULL) OR (event_kind='cleared' AND cleared_at >= effective_at)),
    UNIQUE(tenant_id,id),
    UNIQUE(tenant_id,cause,source_id,source_version,event_kind)
);
CREATE TRIGGER restriction_events_immutable BEFORE UPDATE OR DELETE ON restriction_events
FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE current_restrictions (
    tenant_id text NOT NULL REFERENCES tenants(id),
    cause text NOT NULL,
    source_id text NOT NULL,
    source_version bigint NOT NULL CHECK (source_version > 0),
    event_id text NOT NULL,
    effective_at timestamptz NOT NULL,
    cleared_at timestamptz CHECK (cleared_at >= effective_at),
    PRIMARY KEY(tenant_id,cause,source_id),
    FOREIGN KEY(tenant_id,event_id) REFERENCES restriction_events(tenant_id,id)
);
CREATE INDEX restrictions_active ON current_restrictions(tenant_id,effective_at) WHERE cleared_at IS NULL;
ALTER TABLE audit_events ADD CONSTRAINT audit_identity_nonempty CHECK (tenant_id<>'' AND actor_id<>'' AND action<>'' AND request_id<>'' AND reason<>'');
