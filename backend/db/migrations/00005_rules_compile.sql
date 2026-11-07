-- +goose Up
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
-- +goose StatementBegin
CREATE FUNCTION bind_rule_compile_input_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.input_digest IS NULL AND NEW.input_digest IS NOT NULL
    AND (to_jsonb(OLD)-'input_digest')=(to_jsonb(NEW)-'input_digest') THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'immutable rule compile request';
END;
$$;
-- +goose StatementEnd
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
-- +goose Down
ALTER TABLE operations DROP CONSTRAINT operations_state_check;
ALTER TABLE operations ADD CONSTRAINT operations_state_check CHECK(state IN ('pending','leased','running','succeeded','failed','unknown'));
ALTER TABLE tasks DROP CONSTRAINT tasks_task_kind_check;
ALTER TABLE tasks DROP CONSTRAINT tasks_state_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_state_check CHECK(state IN ('pending','leased','running','succeeded','failed','unknown'));
ALTER TABLE tasks ADD CONSTRAINT tasks_task_kind_check CHECK(task_kind IN ('control','resource_change','port_prepare','port_release'));
ALTER TABLE rule_compile_requests DROP CONSTRAINT rule_compile_input_binding;
DROP TABLE rule_delivery_receipts,rule_delivery_bindings,rule_deployment_members,rule_deployments,rule_artifact_commits,rule_artifacts,rule_compile_inputs,rule_compile_requests,rule_dependencies,rule_segments,rule_targets,rule_revisions,rule_heads;
DROP FUNCTION bind_rule_compile_input_once();
