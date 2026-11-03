-- DRAFT 1.0.0; not approved or frozen.
-- Source: V1.11 §0.2, TEN-001..009, NOD-001..014, BE-001..006, AGT-003,
-- HUB-003, HUB-012 and Appendix B. Entity lifecycle and several exact field
-- definitions remain underspecified and are listed in contracts/OPEN-QUESTIONS.md.

CREATE TABLE tenants (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE users (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    status text NOT NULL CHECK (status IN ('active', 'suspended', 'banned')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE nodes (
    id uuid PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    owner_kind text NOT NULL CHECK (owner_kind IN ('platform', 'tenant')),
    lifecycle text NOT NULL CHECK (lifecycle IN ('pending', 'online', 'offline', 'draining', 'disabled')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK ((owner_kind = 'platform' AND tenant_id IS NULL) OR
           (owner_kind = 'tenant' AND tenant_id IS NOT NULL))
);

CREATE TABLE node_groups (
    id uuid PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    owner_kind text NOT NULL CHECK (owner_kind IN ('platform', 'tenant')),
    purpose text NOT NULL CHECK (purpose IN ('entry', 'relay', 'exit')),
    billing_multiplier numeric(12, 3) NOT NULL CHECK (billing_multiplier >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK ((owner_kind = 'platform' AND tenant_id IS NULL) OR
           (owner_kind = 'tenant' AND tenant_id IS NOT NULL)),
    CHECK ((owner_kind = 'tenant' AND billing_multiplier = 0) OR
           (owner_kind = 'platform' AND billing_multiplier >= 0.001))
);

CREATE TABLE node_group_members (
    node_group_id uuid NOT NULL REFERENCES node_groups(id),
    node_id uuid NOT NULL REFERENCES nodes(id),
    joined_at timestamptz NOT NULL,
    PRIMARY KEY (node_group_id, node_id)
);

CREATE TABLE segment_permits (
    id uuid PRIMARY KEY,
    from_group_id uuid NOT NULL REFERENCES node_groups(id),
    to_group_id uuid NOT NULL REFERENCES node_groups(id),
    enabled boolean NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (from_group_id, to_group_id)
);

-- DRAFT GAP: V1.11 requires PostgreSQL as the sole business authority, tenant
-- isolation, durable Outbox, billing snapshots, grants, reservations, ledger
-- and idempotent usage batches; exact complete entity/column schemas and
-- retention/index policies must be resolved before this file can be frozen.
