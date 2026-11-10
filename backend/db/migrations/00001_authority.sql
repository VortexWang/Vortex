-- +goose Up
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
-- +goose StatementBegin
CREATE FUNCTION reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audit events are immutable'; END;
$$;
-- +goose StatementEnd
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
-- +goose Down
DROP TABLE control_observations, control_receipts, audit_events, outbox, tasks, operations, desired_state;
DROP FUNCTION reject_audit_mutation();
