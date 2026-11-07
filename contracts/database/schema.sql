-- Phase 6 implemented PostgreSQL schema through migration 00003.
-- Canonical executable source: backend/db/migrations/*.sql.

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
CREATE TRIGGER node_authority BEFORE INSERT OR UPDATE ON nodes FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
CREATE TRIGGER group_authority BEFORE INSERT OR UPDATE ON node_groups FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
CREATE TRIGGER member_authority BEFORE INSERT OR UPDATE ON node_group_members FOR EACH ROW EXECUTE FUNCTION check_node_group_authority();
CREATE TABLE port_allocations (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), rule_id text NOT NULL,
 group_id text NOT NULL REFERENCES node_groups(id), member_set_version text NOT NULL CHECK(length(member_set_version)=64),
 port integer NOT NULL CHECK(port BETWEEN 1 AND 65535), listen_address text NOT NULL CHECK(listen_address<>''),
 protocols text[] NOT NULL CHECK(cardinality(protocols) BETWEEN 1 AND 2 AND protocols <@ ARRAY['tcp','udp']::text[]),
 purpose text NOT NULL CHECK(purpose IN ('service','link','control','metrics')),
 state text NOT NULL CHECK(state IN ('preparing','committed','aborting','retained','releasing','released','aborted','unknown')),
 first_effective_at timestamptz, retained_until timestamptz,
 needs_revalidation boolean NOT NULL DEFAULT false, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 UNIQUE(tenant_id,id), CHECK((first_effective_at IS NULL AND retained_until IS NULL) OR retained_until=first_effective_at+interval '7 days')
);
CREATE UNIQUE INDEX port_rule_active ON port_allocations(tenant_id,rule_id) WHERE state NOT IN ('released','aborted');
ALTER TABLE tasks ADD CONSTRAINT tasks_tenant_identity UNIQUE(tenant_id,id);
CREATE TABLE port_reservations (
 tenant_id text NOT NULL, allocation_id text NOT NULL, node_id text NOT NULL REFERENCES nodes(id),
 protocol text NOT NULL CHECK(protocol IN ('tcp','udp')), port integer NOT NULL CHECK(port BETWEEN 1 AND 65535),
 listen_address text NOT NULL, boot_id text NOT NULL CHECK(boot_id<>''), generation bigint NOT NULL CHECK(generation>0),
 prepare_task_id text NOT NULL, release_task_id text,
 config_confirmed boolean NOT NULL DEFAULT false,
 state text NOT NULL CHECK(state IN ('prepared','confirmed','retained','releasing','released','unknown')),
 PRIMARY KEY(tenant_id,allocation_id,node_id,protocol),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES port_allocations(tenant_id,id),
 FOREIGN KEY(tenant_id,prepare_task_id) REFERENCES tasks(tenant_id,id),
 FOREIGN KEY(tenant_id,release_task_id) REFERENCES tasks(tenant_id,id)
);
CREATE UNIQUE INDEX port_occupied ON port_reservations(tenant_id,node_id,protocol,port,listen_address) WHERE state<>'released';
CREATE TABLE release_proofs (
 tenant_id text NOT NULL, allocation_id text NOT NULL, node_id text NOT NULL REFERENCES nodes(id),
 task_id text NOT NULL, boot_id text NOT NULL, revision bigint NOT NULL CHECK(revision>0),
 generation bigint NOT NULL CHECK(generation>0), proof_kind text NOT NULL CHECK(proof_kind IN ('prepare','release')),
 receipt_sha256 bytea NOT NULL CHECK(octet_length(receipt_sha256)=32),
 peer_fingerprint bytea NOT NULL CHECK(octet_length(peer_fingerprint)=32),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,allocation_id,node_id,task_id),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES port_allocations(tenant_id,id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES tasks(tenant_id,id)
);
CREATE TRIGGER release_proof_immutable BEFORE UPDATE OR DELETE ON release_proofs FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER ownership_immutable BEFORE UPDATE OR DELETE ON resource_ownership FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Lock the physical node, so wildcard conflicts across different tenants and
-- groups serialize even if a writer bypasses the service candidate search.
CREATE FUNCTION guard_physical_port() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM nodes WHERE id=NEW.node_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM port_allocations a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.allocation_id AND a.port=NEW.port AND a.listen_address=NEW.listen_address AND NEW.protocol=ANY(a.protocols)) THEN RAISE EXCEPTION 'allocation binding mismatch'; END IF;
 IF TG_OP='UPDATE' AND (NEW.tenant_id,NEW.allocation_id,NEW.node_id,NEW.protocol,NEW.port,NEW.listen_address,NEW.boot_id,NEW.generation) IS DISTINCT FROM (OLD.tenant_id,OLD.allocation_id,OLD.node_id,OLD.protocol,OLD.port,OLD.listen_address,OLD.boot_id,OLD.generation) THEN RAISE EXCEPTION 'immutable reservation binding'; END IF;
 IF NEW.state<>'released' AND EXISTS(SELECT 1 FROM port_reservations p WHERE p.node_id=NEW.node_id AND p.protocol=NEW.protocol AND p.port=NEW.port AND p.state<>'released'
  AND (p.listen_address=NEW.listen_address OR p.listen_address IN ('*','0.0.0.0','::') OR NEW.listen_address IN ('*','0.0.0.0','::'))
  AND (p.tenant_id,p.allocation_id,p.node_id,p.protocol) IS DISTINCT FROM (NEW.tenant_id,NEW.allocation_id,NEW.node_id,NEW.protocol)) THEN
  RAISE EXCEPTION 'physical port conflict' USING ERRCODE='23505';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER port_conflict BEFORE INSERT OR UPDATE ON port_reservations FOR EACH ROW EXECUTE FUNCTION guard_physical_port();
