-- +goose Up
ALTER TABLE encrypted_credentials ADD COLUMN zone_name text NOT NULL DEFAULT '';
CREATE TABLE entry_group_domains (
 tenant_id text NOT NULL REFERENCES tenants(id),
 group_id text NOT NULL REFERENCES node_groups(id),
 credential_id text NOT NULL,
 zone_id text NOT NULL,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 253),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(group_id),
 UNIQUE(name),
 FOREIGN KEY(tenant_id,credential_id) REFERENCES encrypted_credentials(tenant_id,id)
);
CREATE TRIGGER entry_group_domains_immutable BEFORE UPDATE OR DELETE ON entry_group_domains FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();

-- +goose Down
DROP TRIGGER entry_group_domains_immutable ON entry_group_domains;
DROP TABLE entry_group_domains;
ALTER TABLE encrypted_credentials DROP COLUMN zone_name;
