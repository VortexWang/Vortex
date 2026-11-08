-- +goose Up
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
-- +goose Down
-- Authority histories are intentionally not rolled back into reusable rights.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'share authority migration is irreversible; restore requires authority reconciliation'; END $$;
-- +goose StatementEnd
