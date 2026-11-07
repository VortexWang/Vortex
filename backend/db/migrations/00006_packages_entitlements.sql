-- +goose Up
-- Additive upgrade only. Issuance is not funded ledger authority (00007).
CREATE TABLE package_catalog (
 id text PRIMARY KEY, name text NOT NULL CHECK(length(name) BETWEEN 1 AND 128),
 current_version bigint NOT NULL CHECK(current_version>0), enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE package_versions (
 package_id text NOT NULL REFERENCES package_catalog(id), version bigint NOT NULL CHECK(version>0),
 terms jsonb NOT NULL CHECK(jsonb_typeof(terms)='object'), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(package_id,version)
);
CREATE TRIGGER package_versions_immutable BEFORE UPDATE OR DELETE ON package_versions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE package_snapshots (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), package_id text NOT NULL,
 package_version bigint NOT NULL, terms jsonb NOT NULL CHECK(jsonb_typeof(terms)='object'),
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,id),
 FOREIGN KEY(package_id,package_version) REFERENCES package_versions(package_id,version)
);
CREATE TRIGGER package_snapshots_immutable BEFORE UPDATE OR DELETE ON package_snapshots FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE subscriptions (
 id text PRIMARY KEY, tenant_id text NOT NULL UNIQUE REFERENCES tenants(id), snapshot_id text NOT NULL,
 anchor_at timestamptz NOT NULL, expires_at timestamptz NOT NULL CHECK(expires_at>anchor_at),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES package_snapshots(tenant_id,id)
);
CREATE TABLE traffic_sources (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), snapshot_id text NOT NULL,
 subscription_id text, kind text NOT NULL CHECK(kind IN ('base','traffic')),
 initial_amount bigint NOT NULL CHECK(initial_amount>=0), issued_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>issued_at), period_key text NOT NULL,
 CHECK(kind='traffic' OR subscription_id IS NOT NULL),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,kind,period_key),
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES package_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,subscription_id) REFERENCES subscriptions(tenant_id,id)
);
CREATE INDEX traffic_source_order ON traffic_sources(tenant_id,kind,expires_at,issued_at,id);
CREATE TRIGGER traffic_sources_immutable BEFORE UPDATE OR DELETE ON traffic_sources FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- First and only CREATE of billing_snapshots. The next plan extends this table.
CREATE TABLE billing_snapshots (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), package_snapshot_id text NOT NULL,
 rule_id text NOT NULL, rule_revision bigint NOT NULL CHECK(rule_revision>0), path_generation bigint NOT NULL CHECK(path_generation>0),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), multiplier_milli bigint NOT NULL CHECK(multiplier_milli>=0),
 path_versions jsonb NOT NULL CHECK(jsonb_typeof(path_versions)='array'), created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,rule_id,rule_revision,path_generation,package_snapshot_id),
 FOREIGN KEY(tenant_id,package_snapshot_id) REFERENCES package_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,rule_id,rule_revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
CREATE TRIGGER billing_snapshots_immutable BEFORE UPDATE OR DELETE ON billing_snapshots FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE package_requests (
 request_key text PRIMARY KEY CHECK(length(request_key) BETWEEN 1 AND 128), tenant_id text NOT NULL REFERENCES tenants(id),
 payload_sha256 bytea NOT NULL CHECK(octet_length(payload_sha256)=32), result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER package_requests_immutable BEFORE UPDATE OR DELETE ON package_requests FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE entitlement_codes (
 code_hash bytea PRIMARY KEY CHECK(octet_length(code_hash)=32), kind text NOT NULL CHECK(kind IN ('package','duration','traffic')),
 package_id text NOT NULL, package_version bigint NOT NULL, duration_days integer NOT NULL CHECK(duration_days BETWEEN 1 AND 36500),
 amount bigint NOT NULL CHECK(amount>=0), remaining_uses bigint NOT NULL CHECK(remaining_uses>=0),
 valid_until timestamptz NOT NULL, applicable_tenant text REFERENCES tenants(id), revoked boolean NOT NULL DEFAULT false,
 FOREIGN KEY(package_id,package_version) REFERENCES package_versions(package_id,version)
);
CREATE TABLE entitlement_redemptions (
 request_key text PRIMARY KEY REFERENCES package_requests(request_key), tenant_id text NOT NULL REFERENCES tenants(id),
 code_hash bytea NOT NULL REFERENCES entitlement_codes(code_hash), result jsonb NOT NULL,
 redeemed_at timestamptz NOT NULL DEFAULT now(), UNIQUE(code_hash,request_key)
);
CREATE TRIGGER entitlement_redemptions_immutable BEFORE UPDATE OR DELETE ON entitlement_redemptions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE package_changes (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), subscription_id text NOT NULL,
 snapshot_id text NOT NULL, expected_version bigint NOT NULL CHECK(expected_version>0),
 effective_at timestamptz NOT NULL, preview jsonb NOT NULL, created_by text NOT NULL, task_id text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','succeeded','failed')),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,subscription_id) REFERENCES subscriptions(tenant_id,id),
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES package_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES operations(tenant_id,id)
);
CREATE UNIQUE INDEX one_pending_package_change ON package_changes(tenant_id) WHERE state='pending';
CREATE TABLE source_expirations (
 tenant_id text NOT NULL, source_id text NOT NULL, effective_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(reason IN ('period_end','replacement','expiry')),
 PRIMARY KEY(tenant_id,source_id), FOREIGN KEY(tenant_id,source_id) REFERENCES traffic_sources(tenant_id,id)
);
CREATE TRIGGER source_expirations_immutable BEFORE UPDATE OR DELETE ON source_expirations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release','rule_compile','rule_prepare','rule_commit','rule_abort','rule_query','package_due'));
-- +goose Down
-- Issuance history and in-flight authority must never be silently discarded.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00006 package authority is forward-only'; END $$;
-- +goose StatementEnd
