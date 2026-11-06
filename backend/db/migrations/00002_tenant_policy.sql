-- +goose Up
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
-- +goose Down
ALTER TABLE audit_events DROP CONSTRAINT audit_identity_nonempty;
DROP TABLE current_restrictions, restriction_events, resource_grants, resource_ownership, platform_principals, users, tenants;
