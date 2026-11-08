-- Phase 6 implemented PostgreSQL schema through migration 00008_limits_shares.
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


CREATE TABLE rule_heads (
 tenant_id text NOT NULL REFERENCES tenants(id), id text NOT NULL,
 desired_revision bigint NOT NULL DEFAULT 0 CHECK(desired_revision>=0),
 active_revision bigint NOT NULL DEFAULT 0 CHECK(active_revision>=0),
 sync_status text NOT NULL DEFAULT 'pending' CHECK(sync_status IN ('pending','preparing','committing','running','partial','unknown','failed')),
 PRIMARY KEY(tenant_id,id), CHECK(active_revision<=desired_revision)
);
CREATE TABLE rule_revisions (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision bigint NOT NULL CHECK(revision>0),
 payload bytea NOT NULL CHECK(octet_length(payload) BETWEEN 1 AND 524288),
 payload_sha256 bytea NOT NULL CHECK(octet_length(payload_sha256)=32),
 created_by text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 change_class text NOT NULL CHECK(change_class IN ('hot','rebuild','stop')),
 rollback_of bigint NOT NULL DEFAULT 0 CHECK(rollback_of>=0),
 PRIMARY KEY(tenant_id,rule_id,revision), FOREIGN KEY(tenant_id,rule_id) REFERENCES rule_heads(tenant_id,id)
);
CREATE TRIGGER rule_revision_immutable BEFORE UPDATE OR DELETE ON rule_revisions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE rule_targets (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision bigint NOT NULL,
 address text NOT NULL, weight integer NOT NULL CHECK(weight BETWEEN 1 AND 10000), priority integer NOT NULL CHECK(priority>=0),
 PRIMARY KEY(tenant_id,rule_id,revision,address),
 FOREIGN KEY(tenant_id,rule_id,revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
CREATE TABLE rule_segments (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision bigint NOT NULL, hop integer NOT NULL CHECK(hop>=0),
 from_group_id text NOT NULL REFERENCES node_groups(id), to_group_id text NOT NULL REFERENCES node_groups(id),
 direction text NOT NULL CHECK(direction IN ('forward','reverse')), permit_version bigint NOT NULL CHECK(permit_version>=0),
 PRIMARY KEY(tenant_id,rule_id,revision,hop), CHECK(from_group_id<>to_group_id),
 FOREIGN KEY(tenant_id,rule_id,revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
CREATE TABLE rule_dependencies (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision bigint NOT NULL,
 dependency_kind text NOT NULL CHECK(dependency_kind IN ('group','grant','permit','billing')),
 dependency_id text NOT NULL, version text NOT NULL,
 PRIMARY KEY(tenant_id,rule_id,revision,dependency_kind,dependency_id),
 FOREIGN KEY(tenant_id,rule_id,revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
CREATE INDEX rule_dependencies_lookup ON rule_dependencies(dependency_kind,dependency_id,tenant_id);
CREATE TRIGGER rule_targets_immutable BEFORE UPDATE OR DELETE ON rule_targets FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER rule_segments_immutable BEFORE UPDATE OR DELETE ON rule_segments FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER rule_dependencies_immutable BEFORE UPDATE OR DELETE ON rule_dependencies FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE rule_compile_requests (
 tenant_id text NOT NULL, task_id text NOT NULL, rule_id text NOT NULL, rule_revision bigint NOT NULL,
 input_digest text CHECK(input_digest ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,task_id), FOREIGN KEY(tenant_id,task_id) REFERENCES tasks(tenant_id,id),
 FOREIGN KEY(tenant_id,rule_id,rule_revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
CREATE INDEX rule_compile_request_rule ON rule_compile_requests(tenant_id,rule_id,rule_revision);
CREATE FUNCTION bind_rule_compile_input_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.input_digest IS NULL AND NEW.input_digest IS NOT NULL
    AND (to_jsonb(OLD)-'input_digest')=(to_jsonb(NEW)-'input_digest') THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'immutable rule compile request';
END;
$$;
CREATE TRIGGER rule_compile_request_immutable BEFORE UPDATE OR DELETE ON rule_compile_requests FOR EACH ROW EXECUTE FUNCTION bind_rule_compile_input_once();
ALTER TABLE tasks DROP CONSTRAINT tasks_state_check;
ALTER TABLE operations DROP CONSTRAINT operations_state_check;
ALTER TABLE operations ADD CONSTRAINT operations_state_check CHECK(state IN ('pending','leased','running','succeeded','failed','unknown','superseded'));
ALTER TABLE tasks ADD CONSTRAINT tasks_state_check CHECK(state IN ('pending','leased','running','succeeded','failed','unknown','superseded'));
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release','rule_compile','rule_prepare','rule_commit','rule_abort','rule_query'));
CREATE TABLE rule_compile_inputs (
 tenant_id text NOT NULL, rule_id text NOT NULL, rule_revision bigint NOT NULL,
 input_digest text NOT NULL CHECK(input_digest ~ '^[0-9a-f]{64}$'),
 canonical bytea NOT NULL CHECK(octet_length(canonical) BETWEEN 1 AND 524288),
 billing_snapshot_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 path_generation bigint NOT NULL CHECK(path_generation>0), fencing_generation bigint NOT NULL CHECK(fencing_generation>0),
 PRIMARY KEY(tenant_id,rule_id,rule_revision,input_digest),
 FOREIGN KEY(tenant_id,rule_id,rule_revision) REFERENCES rule_revisions(tenant_id,rule_id,revision)
);
ALTER TABLE rule_compile_requests ADD CONSTRAINT rule_compile_input_binding
 FOREIGN KEY(tenant_id,rule_id,rule_revision,input_digest) REFERENCES rule_compile_inputs(tenant_id,rule_id,rule_revision,input_digest);
CREATE TABLE rule_artifacts (
 tenant_id text NOT NULL, rule_id text NOT NULL, rule_revision bigint NOT NULL,
 node_id text NOT NULL REFERENCES nodes(id), input_digest text NOT NULL,
 digest text NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 canonical bytea NOT NULL CHECK(octet_length(canonical) BETWEEN 1 AND 524288),
 billing_snapshot_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 path_generation bigint NOT NULL CHECK(path_generation>0), fencing_generation bigint NOT NULL CHECK(fencing_generation>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,rule_id,rule_revision,node_id,input_digest),
 UNIQUE(tenant_id,digest),
 FOREIGN KEY(tenant_id,rule_id,rule_revision,input_digest) REFERENCES rule_compile_inputs(tenant_id,rule_id,rule_revision,input_digest)
);
CREATE TABLE rule_artifact_commits (
 tenant_id text NOT NULL, digest text NOT NULL, committed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,digest), FOREIGN KEY(tenant_id,digest) REFERENCES rule_artifacts(tenant_id,digest)
);
CREATE TRIGGER rule_input_immutable BEFORE UPDATE OR DELETE ON rule_compile_inputs FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER rule_artifact_immutable BEFORE UPDATE OR DELETE ON rule_artifacts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER rule_artifact_commit_immutable BEFORE UPDATE OR DELETE ON rule_artifact_commits FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE rule_deployments (
 tenant_id text NOT NULL, id text NOT NULL, rule_id text NOT NULL, rule_revision bigint NOT NULL,
 input_digest text NOT NULL, state text NOT NULL CHECK(state IN ('preparing','committing','running','partial','unknown','failed','superseded')),
 deadline timestamptz NOT NULL, rollback_revision bigint NOT NULL DEFAULT 0, created_by text NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,rule_id,rule_revision,input_digest),
 FOREIGN KEY(tenant_id,rule_id,rule_revision,input_digest) REFERENCES rule_compile_inputs(tenant_id,rule_id,rule_revision,input_digest)
);
CREATE TABLE rule_deployment_members (
 tenant_id text NOT NULL, deployment_id text NOT NULL, node_id text NOT NULL,
 boot_id text NOT NULL, transaction_id text NOT NULL, artifact_digest text NOT NULL,
 fencing_generation bigint NOT NULL, state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','prepared','active','aborted','unknown','failed')),
 PRIMARY KEY(tenant_id,deployment_id,node_id), UNIQUE(transaction_id),
 FOREIGN KEY(tenant_id,deployment_id) REFERENCES rule_deployments(tenant_id,id),
 FOREIGN KEY(tenant_id,artifact_digest) REFERENCES rule_artifacts(tenant_id,digest)
);
CREATE TABLE rule_delivery_bindings (
 tenant_id text NOT NULL, task_id text NOT NULL, deployment_id text NOT NULL, node_id text NOT NULL,
 phase text NOT NULL CHECK(phase IN ('prepare','commit','query','abort')),
 PRIMARY KEY(tenant_id,task_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES tasks(tenant_id,id),
 FOREIGN KEY(tenant_id,deployment_id,node_id) REFERENCES rule_deployment_members(tenant_id,deployment_id,node_id)
);
CREATE TABLE rule_delivery_receipts (
 tenant_id text NOT NULL, task_id text NOT NULL, receipt_sha256 bytea NOT NULL CHECK(octet_length(receipt_sha256)=32),
 payload bytea NOT NULL CHECK(octet_length(payload) BETWEEN 1 AND 1048576),
 PRIMARY KEY(tenant_id,task_id), FOREIGN KEY(tenant_id,task_id) REFERENCES rule_delivery_bindings(tenant_id,task_id)
);
CREATE TRIGGER rule_binding_immutable BEFORE UPDATE OR DELETE ON rule_delivery_bindings FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER rule_delivery_receipt_immutable BEFORE UPDATE OR DELETE ON rule_delivery_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();

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
-- Package restrictions can stop one rule without suspending compliant siblings.
ALTER TABLE restriction_events ADD COLUMN resource_id text;
ALTER TABLE current_restrictions ADD COLUMN resource_id text;
ALTER TABLE restriction_events ADD FOREIGN KEY(tenant_id,resource_id) REFERENCES rule_heads(tenant_id,id);
ALTER TABLE current_restrictions ADD FOREIGN KEY(tenant_id,resource_id) REFERENCES rule_heads(tenant_id,id);
ALTER TABLE restriction_events ADD CHECK(resource_id IS NULL OR cause='quantity_exceeded');
ALTER TABLE current_restrictions ADD CHECK(resource_id IS NULL OR cause='quantity_exceeded');
ALTER TABLE package_changes ADD COLUMN created_by_platform boolean NOT NULL DEFAULT false;
CREATE TABLE source_expirations (
 tenant_id text NOT NULL, source_id text NOT NULL, effective_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(reason IN ('period_end','replacement','expiry')),
 PRIMARY KEY(tenant_id,source_id), FOREIGN KEY(tenant_id,source_id) REFERENCES traffic_sources(tenant_id,id)
);
CREATE TRIGGER source_expirations_immutable BEFORE UPDATE OR DELETE ON source_expirations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE source_extensions (
 tenant_id text NOT NULL, source_id text NOT NULL, request_key text NOT NULL,
 expires_at timestamptz NOT NULL, PRIMARY KEY(tenant_id,source_id,request_key),
 FOREIGN KEY(tenant_id,source_id) REFERENCES traffic_sources(tenant_id,id)
);
CREATE TRIGGER source_extensions_immutable BEFORE UPDATE OR DELETE ON source_extensions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release','rule_compile','rule_prepare','rule_commit','rule_abort','rule_query','package_due'));

-- 00001..00006 are immutable applied history. No second billing_snapshots table.
ALTER TABLE billing_snapshots ADD COLUMN valid_from timestamptz NOT NULL DEFAULT '-infinity';
ALTER TABLE billing_snapshots ADD COLUMN valid_until timestamptz NOT NULL DEFAULT 'infinity';
ALTER TABLE billing_snapshots ADD COLUMN announced_at timestamptz;
ALTER TABLE billing_snapshots ADD COLUMN synchronized_at timestamptz;
ALTER TABLE billing_snapshots ADD CHECK(valid_from<valid_until);
ALTER TABLE billing_snapshots ADD CHECK(announced_at IS NULL OR (synchronized_at IS NOT NULL AND synchronized_at>=announced_at AND valid_from>=synchronized_at+interval '24 hours'));
CREATE FUNCTION billing_vector_coefficient(vector jsonb) RETURNS bigint LANGUAGE SQL IMMUTABLE AS $$ SELECT coalesce(sum((g->>'multiplier_milli')::bigint),0)::bigint FROM jsonb_array_elements(vector) g $$;
ALTER TABLE billing_snapshots ADD CHECK(multiplier_milli=billing_vector_coefficient(path_versions));
ALTER TABLE billing_snapshots DROP CONSTRAINT billing_snapshots_tenant_id_rule_id_rule_revision_path_gene_key;
ALTER TABLE billing_snapshots ADD UNIQUE(tenant_id,rule_id,rule_revision,path_generation,package_snapshot_id,version);
CREATE TABLE source_ledgers (
 tenant_id text NOT NULL, source_id text NOT NULL, origin jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,source_id),
 FOREIGN KEY(tenant_id,source_id) REFERENCES traffic_sources(tenant_id,id)
);
CREATE TRIGGER source_ledgers_immutable BEFORE UPDATE OR DELETE ON source_ledgers FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE ledger_projections (
 tenant_id text NOT NULL, source_id text NOT NULL, b bigint NOT NULL DEFAULT 0 CHECK(b>=0),
 f bigint NOT NULL DEFAULT 0 CHECK(f>=0), l bigint NOT NULL DEFAULT 0 CHECK(l>=0),
 charged bigint NOT NULL DEFAULT 0 CHECK(charged>=0), remainder_milli bigint NOT NULL DEFAULT 0 CHECK(remainder_milli BETWEEN 0 AND 999),
 sequence bigint NOT NULL DEFAULT 0, digest bytea NOT NULL DEFAULT decode(repeat('00',32),'hex'),
 PRIMARY KEY(tenant_id,source_id), FOREIGN KEY(tenant_id,source_id) REFERENCES source_ledgers(tenant_id,source_id),
 CHECK(f::numeric<=b::numeric+l::numeric)
);
CREATE TABLE ledger_events (
 tenant_id text NOT NULL, source_id text NOT NULL, sequence bigint NOT NULL, event_key text NOT NULL,
 billing_snapshot_id text, kind text NOT NULL, delta_b bigint NOT NULL, delta_f bigint NOT NULL,
 delta_l bigint NOT NULL, delta_charged bigint NOT NULL DEFAULT 0, delta_remainder bigint NOT NULL DEFAULT 0,
 evidence jsonb NOT NULL CHECK(jsonb_typeof(evidence)='object'), previous_digest bytea NOT NULL, digest bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,source_id,sequence), UNIQUE(tenant_id,source_id,event_key),
 FOREIGN KEY(tenant_id,source_id) REFERENCES source_ledgers(tenant_id,source_id),
 FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id),
 CHECK(billing_snapshot_id IS NOT NULL OR kind IN ('origin','expiry','guarantee'))
);
CREATE TRIGGER ledger_events_immutable BEFORE UPDATE OR DELETE ON ledger_events FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE FUNCTION apply_source_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p ledger_projections;
BEGIN
 SELECT * INTO STRICT p FROM ledger_projections WHERE tenant_id=NEW.tenant_id AND source_id=NEW.source_id FOR UPDATE;
 NEW.sequence:=p.sequence+1;
 NEW.previous_digest:=p.digest;
 NEW.digest:=sha256(p.digest || convert_to(jsonb_build_array(NEW.tenant_id,NEW.source_id,NEW.sequence,NEW.event_key,NEW.billing_snapshot_id,NEW.kind,NEW.delta_b,NEW.delta_f,NEW.delta_l,NEW.delta_charged,NEW.delta_remainder,NEW.evidence)::text,'UTF8'));
 UPDATE ledger_projections SET b=b+NEW.delta_b,f=f+NEW.delta_f,l=l+NEW.delta_l,charged=charged+NEW.delta_charged,remainder_milli=remainder_milli+NEW.delta_remainder,sequence=NEW.sequence,digest=NEW.digest WHERE tenant_id=NEW.tenant_id AND source_id=NEW.source_id;
 RETURN NEW;
END $$;
CREATE TRIGGER apply_ledger_event BEFORE INSERT ON ledger_events FOR EACH ROW EXECUTE FUNCTION apply_source_event();
CREATE FUNCTION verify_source_events(t text,s text) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE e ledger_events; d bytea:=decode(repeat('00',32),'hex'); n bigint:=0;
BEGIN
 FOR e IN SELECT * FROM ledger_events WHERE tenant_id=t AND source_id=s ORDER BY sequence LOOP
  n:=n+1;
  IF e.sequence<>n OR e.previous_digest<>d THEN RETURN false; END IF;
  d:=sha256(d || convert_to(jsonb_build_array(e.tenant_id,e.source_id,e.sequence,e.event_key,e.billing_snapshot_id,e.kind,e.delta_b,e.delta_f,e.delta_l,e.delta_charged,e.delta_remainder,e.evidence)::text,'UTF8'));
  IF e.digest<>d THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;
CREATE TABLE reservations (
 id text PRIMARY KEY, tenant_id text NOT NULL, source_id text NOT NULL, billing_snapshot_id text NOT NULL,
 node_id text NOT NULL, boot_id text NOT NULL, request_nonce text NOT NULL, raw_granted bigint NOT NULL CHECK(raw_granted>0),
 coefficient_milli bigint NOT NULL CHECK(coefficient_milli>0), reserved_bill bigint NOT NULL CHECK(reserved_bill>0),
 valid_until timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,node_id,boot_id,request_nonce),
 FOREIGN KEY(tenant_id,source_id) REFERENCES source_ledgers(tenant_id,source_id),
 FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE TRIGGER reservations_immutable BEFORE UPDATE OR DELETE ON reservations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE settlements (
 tenant_id text NOT NULL, grant_id text NOT NULL, billing_snapshot_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('actual','return','final','conservative','late_exact')),
 receipt_key text NOT NULL, raw_cumulative bigint NOT NULL CHECK(raw_cumulative>=0), returned_raw bigint NOT NULL DEFAULT 0 CHECK(returned_raw>=0),
 billable_numerator numeric(40,0) NOT NULL CHECK(billable_numerator>=0), evidence jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,grant_id,receipt_key),
 FOREIGN KEY(tenant_id,grant_id) REFERENCES reservations(tenant_id,id), FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE UNIQUE INDEX one_terminal_settlement ON settlements(tenant_id,grant_id) WHERE kind IN ('final','conservative');
CREATE UNIQUE INDEX one_late_exact ON settlements(tenant_id,grant_id) WHERE kind='late_exact';
CREATE TRIGGER settlements_immutable BEFORE UPDATE OR DELETE ON settlements FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE disputes (
 id text PRIMARY KEY, tenant_id text NOT NULL, grant_id text NOT NULL, billing_snapshot_id text NOT NULL,
 evidence_sha256 bytea NOT NULL CHECK(octet_length(evidence_sha256)=32), reason text NOT NULL,
 evidence jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,grant_id,evidence_sha256), FOREIGN KEY(tenant_id,grant_id) REFERENCES reservations(tenant_id,id),
 FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE TRIGGER disputes_immutable BEFORE UPDATE OR DELETE ON disputes FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE unknown_liabilities (
 tenant_id text NOT NULL, grant_id text NOT NULL, billing_snapshot_id text NOT NULL,
 confirmed_raw bigint NOT NULL CHECK(confirmed_raw>=0), unknown_raw bigint NOT NULL CHECK(unknown_raw>=0),
 unknown_bill bigint NOT NULL CHECK(unknown_bill>=0), evidence jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,grant_id),
 FOREIGN KEY(tenant_id,grant_id) REFERENCES reservations(tenant_id,id), FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE TRIGGER unknown_liabilities_immutable BEFORE UPDATE OR DELETE ON unknown_liabilities FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE billing_policy_boundaries (
 tenant_id text NOT NULL, old_snapshot_id text NOT NULL, new_snapshot_id text NOT NULL,
 boundary_at timestamptz NOT NULL, evidence jsonb NOT NULL,
 PRIMARY KEY(tenant_id,old_snapshot_id), UNIQUE(tenant_id,new_snapshot_id),
 FOREIGN KEY(tenant_id,old_snapshot_id) REFERENCES billing_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,new_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE TRIGGER billing_policy_boundaries_immutable BEFORE UPDATE OR DELETE ON billing_policy_boundaries FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE usage_batches (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL, boot_id text NOT NULL,
 sequence bigint NOT NULL CHECK(sequence>0), batch_id text NOT NULL, billing_snapshot_id text NOT NULL,
 payload_sha256 bytea NOT NULL CHECK(octet_length(payload_sha256)=32), payload bytea NOT NULL,
 raw_cumulative numeric(40,0) NOT NULL CHECK(raw_cumulative>=0), billable_numerator numeric(40,0) NOT NULL CHECK(billable_numerator>=0),
 final_receipt jsonb, unknown_upper_bound numeric(40,0) NOT NULL DEFAULT 0,
 acknowledgement jsonb NOT NULL, facts jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(node_id,boot_id,sequence), UNIQUE(node_id,boot_id,batch_id),
 FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id)
);
CREATE TRIGGER usage_batches_immutable BEFORE UPDATE OR DELETE ON usage_batches FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE usage_conflicts (
 node_id text NOT NULL, boot_id text NOT NULL, sequence bigint NOT NULL, observed_digest bytea NOT NULL,
 tenant_id text NOT NULL REFERENCES tenants(id), evidence jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(node_id,boot_id,sequence,observed_digest),
 FOREIGN KEY(node_id,boot_id,sequence) REFERENCES usage_batches(node_id,boot_id,sequence)
);
CREATE TRIGGER usage_conflicts_immutable BEFORE UPDATE OR DELETE ON usage_conflicts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
ALTER TABLE settlements ADD COLUMN charged_delta bigint NOT NULL DEFAULT 0 CHECK(charged_delta>=0);
ALTER TABLE settlements ADD COLUMN released_bill bigint NOT NULL DEFAULT 0 CHECK(released_bill>=0);
CREATE TABLE risk_budget_funding (
 id text PRIMARY KEY, amount bigint NOT NULL CHECK(amount>0), backing_reference text NOT NULL UNIQUE,
 approved_by text NOT NULL, approval_evidence jsonb NOT NULL, valid_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER risk_budget_funding_immutable BEFORE UPDATE OR DELETE ON risk_budget_funding FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE risk_budget_debits (
 budget_id text NOT NULL REFERENCES risk_budget_funding(id), tenant_id text NOT NULL, grant_id text NOT NULL,
 amount bigint NOT NULL CHECK(amount>0), compensation_source_id text NOT NULL, evidence jsonb NOT NULL,
 PRIMARY KEY(tenant_id,grant_id), FOREIGN KEY(tenant_id,grant_id) REFERENCES reservations(tenant_id,id),
 FOREIGN KEY(tenant_id,compensation_source_id) REFERENCES traffic_sources(tenant_id,id)
);
CREATE TRIGGER risk_budget_debits_immutable BEFORE UPDATE OR DELETE ON risk_budget_debits FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE risk_budget_recoveries (
 tenant_id text NOT NULL, grant_id text NOT NULL, amount bigint NOT NULL CHECK(amount>=0), evidence jsonb NOT NULL,
 PRIMARY KEY(tenant_id,grant_id), FOREIGN KEY(tenant_id,grant_id) REFERENCES risk_budget_debits(tenant_id,grant_id)
);
CREATE TRIGGER risk_budget_recoveries_immutable BEFORE UPDATE OR DELETE ON risk_budget_recoveries FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE usage_reconciliation_jobs (
 tenant_id text NOT NULL, grant_id text NOT NULL, task_id text NOT NULL, attempts integer NOT NULL DEFAULT 0,
 next_attempt timestamptz NOT NULL, alerted_at timestamptz, PRIMARY KEY(tenant_id,grant_id),
 FOREIGN KEY(tenant_id,grant_id) REFERENCES reservations(tenant_id,id), FOREIGN KEY(tenant_id,task_id) REFERENCES tasks(tenant_id,id)
);
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release','rule_compile','rule_prepare','rule_commit','rule_abort','rule_query','package_due','usage_reconcile'));

-- One serialized capacity account per tenant is the existing tenants row.
-- A chain never changes boot_id. A new boot requires a separate proved close,
-- not a CAS on an old incarnation; generations and liabilities remain history.
CREATE TABLE share_chains (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL REFERENCES nodes(id),
 kind text NOT NULL CHECK(kind IN ('rate','connections')),
 boot_id text NOT NULL CHECK(boot_id<>''), head_generation bigint NOT NULL DEFAULT 0 CHECK(head_generation>=0),
 active_generation bigint NOT NULL DEFAULT 0 CHECK(active_generation>=0),
 pending_generation bigint NOT NULL DEFAULT 0 CHECK(pending_generation>=0),
 held_amount bigint NOT NULL DEFAULT 0 CHECK(held_amount>=0),
 PRIMARY KEY(tenant_id,node_id,kind), CHECK(active_generation<=head_generation), CHECK(pending_generation<=head_generation)
);
CREATE TABLE node_fencing (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL REFERENCES nodes(id),
 boot_id text NOT NULL CHECK(boot_id<>''), fencing_generation bigint NOT NULL CHECK(fencing_generation>0),
 renewal_closed boolean NOT NULL DEFAULT false,
 closure_proof bytea CHECK(closure_proof IS NULL OR octet_length(closure_proof)=32),
 PRIMARY KEY(tenant_id,node_id,boot_id)
);
CREATE TABLE signer_authorities (
 tenant_id text NOT NULL REFERENCES tenants(id), signer_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 heartbeat_at timestamptz NOT NULL, autonomy_until timestamptz NOT NULL,
 approval_evidence bytea NOT NULL CHECK(octet_length(approval_evidence)=32),
 PRIMARY KEY(tenant_id,signer_id,signer_epoch),
 CHECK(autonomy_until>heartbeat_at AND autonomy_until<=heartbeat_at+interval '12 hours')
);
CREATE TRIGGER signer_authority_immutable BEFORE UPDATE OR DELETE ON signer_authorities FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE share_generations (
 tenant_id text NOT NULL, node_id text NOT NULL, kind text NOT NULL,
 generation bigint NOT NULL CHECK(generation>0), predecessor bigint NOT NULL CHECK(predecessor>=0 AND predecessor=generation-1),
 id text NOT NULL, boot_id text NOT NULL, signer_id text NOT NULL,
 signer_epoch bigint NOT NULL CHECK(signer_epoch>0), path_generation bigint NOT NULL CHECK(path_generation>0),
 fencing_generation bigint NOT NULL CHECK(fencing_generation>0),
 amount bigint NOT NULL CHECK(amount>0), burst_bytes bigint NOT NULL CHECK(burst_bytes>=0),
 billing_snapshot_id text NOT NULL, rule_id text NOT NULL, rule_revision bigint NOT NULL CHECK(rule_revision>0),
 created_at timestamptz NOT NULL, ownership_until timestamptz NOT NULL CHECK(ownership_until>created_at),
 policy_until timestamptz NOT NULL CHECK(policy_until>created_at), autonomy_until timestamptz NOT NULL CHECK(autonomy_until>created_at),
 request_key text NOT NULL, request_digest bytea NOT NULL CHECK(octet_length(request_digest)=32),
 PRIMARY KEY(tenant_id,node_id,kind,generation), UNIQUE(tenant_id,id), UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,node_id,kind) REFERENCES share_chains(tenant_id,node_id,kind),
 FOREIGN KEY(tenant_id,node_id,boot_id) REFERENCES node_fencing(tenant_id,node_id,boot_id),
 FOREIGN KEY(tenant_id,billing_snapshot_id) REFERENCES billing_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,signer_id,signer_epoch) REFERENCES signer_authorities(tenant_id,signer_id,signer_epoch),
 CHECK((kind='rate' AND amount<=1000000000000 AND burst_bytes<=amount/8) OR (kind='connections' AND amount<=1000000 AND burst_bytes=0))
);
CREATE TRIGGER share_generation_immutable BEFORE UPDATE OR DELETE ON share_generations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE share_renewal_authorities (
 tenant_id text NOT NULL, node_id text NOT NULL, kind text NOT NULL, generation bigint NOT NULL,
 signer_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 closed_at timestamptz, closure_proof bytea CHECK(closure_proof IS NULL OR octet_length(closure_proof)=32),
 PRIMARY KEY(tenant_id,node_id,kind,generation,signer_id),
 FOREIGN KEY(tenant_id,node_id,kind,generation) REFERENCES share_generations(tenant_id,node_id,kind,generation),
 CHECK((closed_at IS NULL)=(closure_proof IS NULL))
);
CREATE TABLE share_handoffs (
 tenant_id text NOT NULL, id text NOT NULL, node_id text NOT NULL, kind text NOT NULL, generation bigint NOT NULL,
 operation_id text NOT NULL, created_by text NOT NULL,
 command bytea, command_digest bytea CHECK(command_digest IS NULL OR octet_length(command_digest)=32),
 state text NOT NULL DEFAULT 'prepared' CHECK(state IN ('prepared','closing','cas_pending','activation-unknown','enforced','failed')),
 receipt_digest bytea CHECK(receipt_digest IS NULL OR octet_length(receipt_digest)=32),
 execution_state text NOT NULL DEFAULT 'submitted' CHECK(execution_state IN ('submitted','enforcing','enforced','failed')),
 next_attempt timestamptz NOT NULL DEFAULT now(), attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,node_id,kind,generation),
 FOREIGN KEY(tenant_id,node_id,kind,generation) REFERENCES share_generations(tenant_id,node_id,kind,generation),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TABLE share_delivery_bindings (
 tenant_id text NOT NULL, task_id text NOT NULL, handoff_id text NOT NULL,
 phase text NOT NULL CHECK(phase IN ('cas','query')),
 PRIMARY KEY(tenant_id,task_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES operations(tenant_id,id),
 FOREIGN KEY(tenant_id,handoff_id) REFERENCES share_handoffs(tenant_id,id)
);
CREATE TABLE share_handoff_receipts (
 tenant_id text NOT NULL, handoff_id text NOT NULL, receipt_id text NOT NULL,
 command_digest bytea NOT NULL CHECK(octet_length(command_digest)=32),
 receipt_digest bytea NOT NULL CHECK(octet_length(receipt_digest)=32),
 peer_fingerprint bytea NOT NULL CHECK(octet_length(peer_fingerprint)=32), payload bytea NOT NULL,
 PRIMARY KEY(tenant_id,receipt_id),
 FOREIGN KEY(tenant_id,handoff_id) REFERENCES share_handoffs(tenant_id,id)
);
CREATE TRIGGER share_receipt_immutable BEFORE UPDATE OR DELETE ON share_handoff_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE lease_expectations (
 tenant_id text NOT NULL, id text NOT NULL, node_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('runtime','rate','connections')),
 share_kind text NOT NULL CHECK(share_kind IN ('rate','connections')), generation bigint NOT NULL,
 boot_id text NOT NULL, revision bigint NOT NULL CHECK(revision>0), nonce text NOT NULL CHECK(nonce<>''),
 connection_id text NOT NULL CHECK(connection_id<>''), request_digest bytea NOT NULL CHECK(octet_length(request_digest)=32),
 requested_at timestamptz NOT NULL, issued_at timestamptz NOT NULL,
 ttl_seconds integer NOT NULL CHECK(ttl_seconds=300), renewal_seconds integer NOT NULL CHECK(renewal_seconds=60),
 relative_deadline timestamptz NOT NULL, ownership_until timestamptz NOT NULL,
 policy_until timestamptz NOT NULL, autonomy_until timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,node_id,kind,nonce), UNIQUE(tenant_id,node_id,kind,revision),
 FOREIGN KEY(tenant_id,node_id,share_kind,generation) REFERENCES share_generations(tenant_id,node_id,kind,generation),
 CHECK(issued_at>=requested_at AND issued_at<=requested_at+interval '10 seconds'),
 CHECK(relative_deadline=requested_at+interval '270 seconds'),
 CHECK(relative_deadline>issued_at AND ownership_until>issued_at AND policy_until>issued_at AND autonomy_until>issued_at)
);
CREATE TRIGGER lease_expectation_immutable BEFORE UPDATE OR DELETE ON lease_expectations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release','rule_compile','rule_prepare','rule_commit','rule_abort','rule_query','package_due','usage_reconcile','share_handoff','share_reconcile'));
CREATE INDEX share_handoffs_due ON share_handoffs(next_attempt) WHERE state IN ('cas_pending','activation-unknown');
-- Allocation windows are immutable targets, not executable ownership grants.
CREATE TABLE share_allocation_windows (
 tenant_id text NOT NULL REFERENCES tenants(id), window_at timestamptz NOT NULL,
 payload bytea NOT NULL, payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 created_by text NOT NULL, operation_id text NOT NULL,
 PRIMARY KEY(tenant_id,window_at), FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id),
 CHECK(window_at=date_trunc('minute',window_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
);
CREATE TRIGGER share_window_immutable BEFORE UPDATE OR DELETE ON share_allocation_windows FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE share_allocation_queue (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL REFERENCES nodes(id),
 kind text NOT NULL CHECK(kind IN ('rate','connections')), queued_at timestamptz NOT NULL,
 window_at timestamptz NOT NULL, target bigint NOT NULL CHECK(target>=0),
 state text NOT NULL CHECK(state IN ('queued','target','applying','enforced','ineligible')),
 PRIMARY KEY(tenant_id,node_id,kind), FOREIGN KEY(tenant_id,window_at) REFERENCES share_allocation_windows(tenant_id,window_at)
);
CREATE TABLE share_demand_samples (
 tenant_id text NOT NULL REFERENCES tenants(id), node_id text NOT NULL REFERENCES nodes(id), boot_id text NOT NULL,
 sample_id text NOT NULL, window_at timestamptz NOT NULL, payload bytea NOT NULL,
 rate_demand bigint NOT NULL CHECK(rate_demand>=0), connection_demand bigint NOT NULL CHECK(connection_demand>=0),
 rate_exhaustion_ppm integer NOT NULL CHECK(rate_exhaustion_ppm BETWEEN 0 AND 1000000),
 connection_exhaustion_ppm integer NOT NULL CHECK(connection_exhaustion_ppm BETWEEN 0 AND 1000000),
 PRIMARY KEY(tenant_id,node_id,window_at), UNIQUE(tenant_id,sample_id)
);
CREATE TRIGGER share_demand_immutable BEFORE UPDATE OR DELETE ON share_demand_samples FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- The first authenticated production session binds an explicitly approved
-- signer epoch to one Hub process boot. A new boot needs recovery evidence;
-- a fresh hello or an empty SQLite database cannot reopen that epoch.
CREATE TABLE hub_signer_incarnations (
 hub_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 boot_id text NOT NULL, PRIMARY KEY(hub_id,signer_epoch)
);
CREATE TRIGGER hub_incarnation_immutable BEFORE UPDATE OR DELETE ON hub_signer_incarnations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- A delegation owns one funded parent reservation. Child credits partition
-- raw bytes, never repeat the parent's financial reservation or ceil rounding.
CREATE TABLE hub_funded_allocations (
 tenant_id text NOT NULL, allocation_id text NOT NULL, delegation_id text NOT NULL,
 reservation_id text NOT NULL, hub_id text NOT NULL, hub_boot_id text NOT NULL, node_id text NOT NULL, boot_id text NOT NULL,
 request_key text NOT NULL, request_digest bytea NOT NULL CHECK(octet_length(request_digest)=32),
 payload bytea NOT NULL, payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 operation_id text NOT NULL, created_by text NOT NULL,
 PRIMARY KEY(tenant_id,allocation_id), UNIQUE(allocation_id), UNIQUE(tenant_id,delegation_id),
 UNIQUE(tenant_id,reservation_id), UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,reservation_id) REFERENCES reservations(tenant_id,id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TRIGGER hub_allocation_immutable BEFORE UPDATE OR DELETE ON hub_funded_allocations FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Source-root generations are not Node aggregate generations. Retain both
-- independent aggregate dependencies for exact renewal-closure snapshots.
CREATE TABLE hub_allocation_share_bindings (
 tenant_id text NOT NULL, allocation_id text NOT NULL, node_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('rate','connections')), generation bigint NOT NULL CHECK(generation>0),
 PRIMARY KEY(tenant_id,allocation_id,kind),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id),
 FOREIGN KEY(tenant_id,node_id,kind,generation) REFERENCES share_generations(tenant_id,node_id,kind,generation)
);
CREATE INDEX hub_allocation_share_generation ON hub_allocation_share_bindings(tenant_id,node_id,kind,generation);
CREATE TRIGGER hub_allocation_share_binding_immutable BEFORE UPDATE OR DELETE ON hub_allocation_share_bindings FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE hub_issue_receipts (
 tenant_id text NOT NULL, issue_id text NOT NULL, allocation_id text NOT NULL,
 hub_id text NOT NULL, ledger_sequence bigint NOT NULL CHECK(ledger_sequence>0),
 payload bytea NOT NULL, payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 PRIMARY KEY(tenant_id,issue_id), UNIQUE(hub_id,ledger_sequence),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id)
);
CREATE TRIGGER hub_issue_immutable BEFORE UPDATE OR DELETE ON hub_issue_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE hub_credit_grants (
 tenant_id text NOT NULL, grant_id text NOT NULL, allocation_id text NOT NULL, issue_id text NOT NULL,
 raw_granted bigint NOT NULL CHECK(raw_granted>0),
 PRIMARY KEY(tenant_id,grant_id), UNIQUE(tenant_id,issue_id),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id),
 FOREIGN KEY(tenant_id,issue_id) REFERENCES hub_issue_receipts(tenant_id,issue_id)
);
CREATE TRIGGER hub_credit_immutable BEFORE UPDATE OR DELETE ON hub_credit_grants FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE hub_credit_usage (
 tenant_id text NOT NULL, grant_id text NOT NULL, raw_cumulative bigint NOT NULL CHECK(raw_cumulative>=0),
 PRIMARY KEY(tenant_id,grant_id), FOREIGN KEY(tenant_id,grant_id) REFERENCES hub_credit_grants(tenant_id,grant_id)
);
CREATE TABLE hub_allocation_receipts (
 tenant_id text NOT NULL, allocation_id text NOT NULL, hub_id text NOT NULL, hub_boot_id text NOT NULL,
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 PRIMARY KEY(tenant_id,allocation_id), FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id)
);
CREATE TRIGGER hub_allocation_receipt_immutable BEFORE UPDATE OR DELETE ON hub_allocation_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Direct backend root provisioning precedes usable Hub authorization at a Node.
CREATE TABLE node_source_provisions (
 tenant_id text NOT NULL, allocation_id text NOT NULL, node_id text NOT NULL, boot_id text NOT NULL,
 backend_id text NOT NULL, operation_id text NOT NULL, payload bytea NOT NULL,
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 PRIMARY KEY(tenant_id,allocation_id),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE TRIGGER node_source_provision_immutable BEFORE UPDATE OR DELETE ON node_source_provisions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE node_source_receipts (
 tenant_id text NOT NULL, allocation_id text NOT NULL, payload bytea NOT NULL,
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 PRIMARY KEY(tenant_id,allocation_id),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES node_source_provisions(tenant_id,allocation_id)
);
CREATE TRIGGER node_source_receipt_immutable BEFORE UPDATE OR DELETE ON node_source_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Immutable commands are also a tenant-serialized barrier against new old-share funding.
CREATE TABLE share_renewal_closure_commands (
 tenant_id text NOT NULL, id text NOT NULL, handoff_id text NOT NULL,
 node_id text NOT NULL, boot_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('rate','connections')),
 generation bigint NOT NULL CHECK(generation>0), signer_id text NOT NULL, signer_epoch bigint NOT NULL CHECK(signer_epoch>0),
 hub_boot_id text NOT NULL, part integer NOT NULL CHECK(part>0), parts integer NOT NULL CHECK(parts>=part),
 operation_id text NOT NULL, payload bytea NOT NULL CHECK(octet_length(payload)<=16384),
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 PRIMARY KEY(tenant_id,id), UNIQUE(id), UNIQUE(tenant_id,handoff_id,signer_id,part),
 FOREIGN KEY(tenant_id,handoff_id) REFERENCES share_handoffs(tenant_id,id),
 FOREIGN KEY(tenant_id,node_id,kind,generation,signer_id) REFERENCES share_renewal_authorities(tenant_id,node_id,kind,generation,signer_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES operations(tenant_id,id)
);
CREATE INDEX share_closure_hub_delivery ON share_renewal_closure_commands(signer_id,hub_boot_id,tenant_id,handoff_id,part);
CREATE INDEX share_closure_generation_barrier ON share_renewal_closure_commands(tenant_id,node_id,boot_id,kind,generation,signer_id);
CREATE TRIGGER share_closure_command_immutable BEFORE UPDATE OR DELETE ON share_renewal_closure_commands FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE share_renewal_closure_receipts (
 tenant_id text NOT NULL, command_id text NOT NULL, payload bytea NOT NULL,
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 peer_fingerprint bytea NOT NULL CHECK(octet_length(peer_fingerprint)=32),
 PRIMARY KEY(tenant_id,command_id),
 FOREIGN KEY(tenant_id,command_id) REFERENCES share_renewal_closure_commands(tenant_id,id)
);
CREATE TRIGGER share_closure_receipt_immutable BEFORE UPDATE OR DELETE ON share_renewal_closure_receipts FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
-- Sealing returns only the proven never-issued portion, never child debt.
CREATE TABLE hub_delegation_seals (
 tenant_id text NOT NULL, allocation_id text NOT NULL, seal_id text NOT NULL,
 hub_id text NOT NULL, hub_boot_id text NOT NULL, ledger_sequence bigint NOT NULL CHECK(ledger_sequence>0),
 payload bytea NOT NULL, payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 peer_fingerprint bytea NOT NULL CHECK(octet_length(peer_fingerprint)=32),
 PRIMARY KEY(tenant_id,allocation_id), UNIQUE(tenant_id,seal_id), UNIQUE(hub_id,ledger_sequence),
 FOREIGN KEY(tenant_id,allocation_id) REFERENCES hub_funded_allocations(tenant_id,allocation_id)
);
CREATE TRIGGER hub_delegation_seal_immutable BEFORE UPDATE OR DELETE ON hub_delegation_seals FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
