-- +goose Up
CREATE TABLE nodes (
 id text PRIMARY KEY REFERENCES resource_ownership(resource_id),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
 region text NOT NULL CHECK (region<>''), hub_id text NOT NULL CHECK (hub_id<>''),
 capabilities text[] NOT NULL CHECK (cardinality(capabilities)>0 AND capabilities <@ ARRAY['entry','relay','exit']::text[]),
 public_address text NOT NULL DEFAULT '', program_version text NOT NULL DEFAULT '',
 health text NOT NULL DEFAULT 'unknown' CHECK (health IN ('unknown','healthy','unhealthy')),
 lifecycle text NOT NULL DEFAULT 'pending' CHECK (lifecycle IN ('pending','online','draining','offline','disabled')),
 port_min integer NOT NULL CHECK (port_min BETWEEN 1 AND 65535),
 port_max integer NOT NULL CHECK (port_max BETWEEN port_min AND 65535),
 link_port integer NOT NULL CHECK (link_port BETWEEN 1 AND 65535),
 control_port integer NOT NULL CHECK (control_port BETWEEN 1 AND 65535),
 metrics_port integer NOT NULL CHECK (metrics_port BETWEEN 1 AND 65535),
 capacity bigint NOT NULL CHECK (capacity>0),
 boot_id text NOT NULL DEFAULT '', generation bigint NOT NULL DEFAULT 1 CHECK (generation>0),
 drain_deadline timestamptz, deleted_at timestamptz,
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 CHECK (link_port<>control_port AND link_port<>metrics_port AND control_port<>metrics_port)
);
CREATE TABLE node_groups (
 id text PRIMARY KEY REFERENCES resource_ownership(resource_id), name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
 purpose text NOT NULL CHECK (purpose IN ('entry','relay','exit')),
 scheduling text NOT NULL CHECK (scheduling IN ('dns','weighted_round_robin','primary_backup')),
 health_interval_seconds integer NOT NULL CHECK (health_interval_seconds BETWEEN 1 AND 3600),
 health_timeout_seconds integer NOT NULL CHECK (health_timeout_seconds BETWEEN 1 AND health_interval_seconds),
 billing_multiplier_milli bigint NOT NULL CHECK (billing_multiplier_milli>=0),
 billing_version bigint NOT NULL DEFAULT 1 CHECK (billing_version>0),
 member_version bigint NOT NULL DEFAULT 1 CHECK (member_version>0),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0), deleted_at timestamptz,
 CHECK ((purpose='entry' AND scheduling='dns') OR (purpose<>'entry' AND scheduling<>'dns'))
);
CREATE TABLE node_group_members (
 group_id text NOT NULL REFERENCES node_groups(id), node_id text NOT NULL REFERENCES nodes(id),
 weight integer NOT NULL CHECK (weight BETWEEN 1 AND 10000), priority integer NOT NULL CHECK (priority>=0),
 PRIMARY KEY(group_id,node_id)
);
CREATE UNIQUE INDEX resource_grants_one_active ON resource_grants(tenant_id,resource_id) WHERE status='active';
CREATE VIEW platform_grants AS SELECT * FROM resource_grants;
CREATE TABLE grant_history (
 id bigserial PRIMARY KEY, grant_id text NOT NULL, tenant_id text NOT NULL REFERENCES tenants(id),
 resource_id text NOT NULL REFERENCES resource_ownership(resource_id), version bigint NOT NULL CHECK (version>0),
 status text NOT NULL CHECK (status IN ('active','revoked')), valid_from timestamptz NOT NULL, valid_until timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(), UNIQUE(grant_id,status),
 FOREIGN KEY(tenant_id,resource_id,version) REFERENCES resource_grants(tenant_id,resource_id,version)
);
CREATE TRIGGER grant_history_immutable BEFORE UPDATE OR DELETE ON grant_history FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE segment_permits (
 tenant_id text NOT NULL REFERENCES tenants(id), from_group_id text NOT NULL REFERENCES node_groups(id),
 to_group_id text NOT NULL REFERENCES node_groups(id), version bigint NOT NULL CHECK (version>0),
 allowed boolean NOT NULL, PRIMARY KEY(tenant_id,from_group_id,to_group_id), CHECK (from_group_id<>to_group_id)
);
CREATE TABLE node_dependencies (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL REFERENCES nodes(id),
 dependency_id text NOT NULL, kind text NOT NULL CHECK (kind IN ('rule','port','settlement','session')),
 amount bigint NOT NULL CHECK (amount>0), PRIMARY KEY(tenant_id,node_id,kind,dependency_id)
);
CREATE TABLE encrypted_credentials (
 id text NOT NULL, tenant_id text NOT NULL REFERENCES tenants(id), zone_id text NOT NULL CHECK (zone_id<>''),
 ciphertext bytea NOT NULL CHECK (octet_length(ciphertext)>16), nonce bytea NOT NULL CHECK (octet_length(nonce)=12),
 key_version bigint NOT NULL CHECK (key_version>0), scope text[] NOT NULL CHECK (cardinality(scope)>0),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,zone_id)
);
ALTER TABLE tasks ADD COLUMN task_kind text NOT NULL DEFAULT 'control' CHECK (task_kind IN ('control','resource_change','port_prepare','port_release'));
-- Ownership and billing constraints live below every service entry point.
-- +goose StatementBegin
CREATE FUNCTION check_node_group_authority() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE g resource_ownership; n resource_ownership; purpose_value text; cap text[]; addr text;
BEGIN
 IF TG_TABLE_NAME='node_groups' THEN
  SELECT * INTO g FROM resource_ownership WHERE resource_id=NEW.id;
  IF g.resource_kind<>'node_group' OR (g.owner_kind='tenant' AND NEW.billing_multiplier_milli<>0) OR (g.owner_kind='platform' AND NEW.billing_multiplier_milli<1) THEN RAISE EXCEPTION 'invalid group ownership/billing'; END IF;
  IF TG_OP='UPDATE' AND (NEW.billing_multiplier_milli<>OLD.billing_multiplier_milli OR NEW.billing_version<>OLD.billing_version) THEN RAISE EXCEPTION 'billing snapshot version required'; END IF;
 ELSIF TG_TABLE_NAME='nodes' THEN
  SELECT * INTO n FROM resource_ownership WHERE resource_id=NEW.id;
  IF n.resource_kind<>'node' THEN RAISE EXCEPTION 'invalid node ownership'; END IF;
  IF TG_OP='UPDATE' AND EXISTS(SELECT 1 FROM node_group_members m JOIN node_groups x ON x.id=m.group_id WHERE m.node_id=NEW.id AND (NOT x.purpose=ANY(NEW.capabilities) OR (x.purpose='entry' AND NEW.public_address=''))) THEN RAISE EXCEPTION 'existing group capability violation'; END IF;
 ELSE
  SELECT * INTO g FROM resource_ownership WHERE resource_id=NEW.group_id;
  SELECT * INTO n FROM resource_ownership WHERE resource_id=NEW.node_id;
  SELECT purpose INTO purpose_value FROM node_groups WHERE id=NEW.group_id AND deleted_at IS NULL;
  SELECT capabilities,public_address INTO cap,addr FROM nodes WHERE id=NEW.node_id AND deleted_at IS NULL;
  IF g.owner_kind<>n.owner_kind OR g.owner_tenant_id IS DISTINCT FROM n.owner_tenant_id OR purpose_value IS NULL OR cap IS NULL OR NOT purpose_value=ANY(cap) OR (purpose_value='entry' AND addr='') THEN RAISE EXCEPTION 'invalid member ownership/capability'; END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER node_authority BEFORE INSERT OR UPDATE ON nodes FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
CREATE TRIGGER group_authority BEFORE INSERT OR UPDATE ON node_groups FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
CREATE TRIGGER member_authority BEFORE INSERT OR UPDATE ON node_group_members FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
-- +goose Down
DROP TRIGGER member_authority ON node_group_members;
DROP TRIGGER group_authority ON node_groups;
DROP TRIGGER node_authority ON nodes;
DROP FUNCTION check_node_group_authority();
ALTER TABLE tasks DROP COLUMN task_kind;
DROP TABLE encrypted_credentials,node_dependencies,segment_permits,grant_history;
DROP VIEW platform_grants;
DROP INDEX resource_grants_one_active;
DROP TABLE node_group_members,node_groups,nodes;
