-- +goose Up
-- 00001..00006 are immutable applied history. No second billing_snapshots table.
ALTER TABLE billing_snapshots ADD COLUMN valid_from timestamptz NOT NULL DEFAULT '-infinity';
ALTER TABLE billing_snapshots ADD COLUMN valid_until timestamptz NOT NULL DEFAULT 'infinity';
ALTER TABLE billing_snapshots ADD COLUMN announced_at timestamptz;
ALTER TABLE billing_snapshots ADD COLUMN synchronized_at timestamptz;
ALTER TABLE billing_snapshots ADD CHECK(valid_from<valid_until);
ALTER TABLE billing_snapshots ADD CHECK(announced_at IS NULL OR (synchronized_at IS NOT NULL AND synchronized_at>=announced_at AND valid_from>=synchronized_at+interval '24 hours'));
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
-- +goose StatementBegin
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
-- +goose StatementEnd
CREATE TRIGGER apply_ledger_event BEFORE INSERT ON ledger_events FOR EACH ROW EXECUTE FUNCTION apply_source_event();
-- +goose StatementBegin
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
-- +goose StatementEnd
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
ALTER TABLE settlements ADD COLUMN charged_delta bigint NOT NULL DEFAULT 0 CHECK(charged_delta>=0);
ALTER TABLE settlements ADD COLUMN released_bill bigint NOT NULL DEFAULT 0 CHECK(released_bill>=0);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00007 immutable accounting is forward-only'; END $$;
-- +goose StatementEnd
