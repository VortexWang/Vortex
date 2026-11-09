package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/reconciliation"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

// This is an explicit finite backend workload: real PG and production service,
// lease, immutable artifact and receipt paths; deterministic healthy-node query
// answers. It measures no transport, Engine throughput or physical fault recovery.
func TestReconcile1000Within60s(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	for _, node := range f.nodes {
		if _, e := f.pool.Exec(ctx, `UPDATE nodes SET port_max=22000 WHERE id=$1`, node); e != nil {
			t.Fatal(e)
		}
	}
	terms := packageTerms()
	terms.RuleLimit = 1000
	subscribePackage(t, f, createPackage(t, f, terms))
	s := service.RuleService{Pool: f.pool}
	o := service.RuleOrchestrator{Pool: f.pool}
	q := generated.New(f.pool)
	type identity struct {
		node, boot      string
		digest          []byte
		revision, fence uint64
	}
	expected := map[string]identity{}
	commands := func(deployment, phase string) []*controlv1.RuleCommand {
		t.Helper()
		rows, e := f.pool.Query(ctx, `SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase=$3 ORDER BY node_id`, f.tenant, deployment, phase)
		if e != nil {
			t.Fatal(e)
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		var out []*controlv1.RuleCommand
		for _, id := range ids {
			c, node, boot, e := service.RuleCommand(ctx, q, f.tenant, id)
			if e != nil {
				t.Fatal(e)
			}
			if phase == "commit" {
				expected[hex.EncodeToString(c.TransactionId)] = identity{node, boot, append([]byte(nil), c.ArtifactSha256...), c.TargetRevision, c.FencingGeneration}
			}
			out = append(out, c)
		}
		return out
	}
	accept := func(c *controlv1.RuleCommand, state string) {
		t.Helper()
		lease := uuid.NewString()
		if _, e := f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token=$3,lease_until=now()+interval '10 seconds' WHERE tenant_id=$1 AND id=$2`, f.tenant, c.TaskKey, lease); e != nil {
			t.Fatal(e)
		}
		if e := o.ReceiptWithLease(ctx, f.scope, c.TaskKey, lease, receipt(c, state), uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	// Fixture creation is outside the normal reconciliation timer. All 1,000
	// desired revisions, compiler inputs, artifacts and query intents are durable.
	for i := 0; i < 1000; i++ {
		in := ruleInput(f.group)
		in.ID = uuid.NewSHA1(uuid.MustParse(f.tenant), []byte(fmt.Sprintf("reconcile/%04d", i))).String()
		in.Name = fmt.Sprintf("qualification-%04d", i)
		in.Port = int32(20000 + i)
		if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
			t.Fatal(i, e)
		}
		reserveRulePort(t, f, in)
		r, e := s.CompileRevision(ctx, f.scope, in.ID, 1, durableBilling(t, f, in.ID, 1, 3), 2, 3, 4, uuid.NewString())
		if e != nil {
			t.Fatal(i, e)
		}
		d, e := o.Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString())
		if e != nil {
			t.Fatal(i, e)
		}
		for _, c := range commands(d.ID, "prepare") {
			accept(c, "prepared")
		}
		for _, c := range commands(d.ID, "commit") {
			accept(c, "unknown")
		}
	}
	if len(expected) != 2000 {
		t.Fatal("incomplete member fixture", len(expected))
	}
	// Lost commit answers require the same-transaction query. No fixture writes
	// are allowed during the timed run; the worker must resolve persisted facts.
	inventory := func() []byte {
		t.Helper()
		var raw []byte
		if e := f.pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(h.id,h.desired_revision,a.node_id,a.digest) ORDER BY h.id,a.node_id)::text::bytea FROM rule_heads h JOIN rule_artifacts a ON a.tenant_id=h.tenant_id AND a.rule_id=h.id AND a.rule_revision=h.desired_revision JOIN rule_artifact_commits c ON c.tenant_id=a.tenant_id AND c.digest=a.digest WHERE h.tenant_id=$1`, f.tenant).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before := inventory()
	digest := sha256.Sum256(before)
	authority := authoritySnapshot(t, f.pool, f.tenant, []string{"billing_snapshots", "package_snapshots", "traffic_sources", "ledger_events", "ledger_projections", "share_handoffs", "share_chains", "rule_revisions", "rule_compile_inputs", "rule_artifacts", "rule_artifact_commits"})
	var observed atomic.Int64
	w := worker.RuleWorker{Pool: f.pool, Adapter: portRuleAdapter(func(_ context.Context, node, boot string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
		id, ok := expected[hex.EncodeToString(c.TransactionId)]
		if !ok || c.Phase != "query" || node != id.node || boot != id.boot || c.TargetRevision != id.revision || c.FencingGeneration != id.fence || !bytes.Equal(c.ArtifactSha256, id.digest) {
			return nil, fmt.Errorf("unexpected immutable query identity")
		}
		observed.Add(1)
		return receipt(c, "active"), nil
	})}
	report := reconciliation.Report{Commit: os.Getenv("PHASE6_EXPECTED_SHA"), Scope: "real PostgreSQL production backend reconciliation; deterministic node adapter; no Engine/transport performance claim", Rules: 1000, Members: 2000, InventorySHA256: hex.EncodeToString(digest[:]), Status: "failed"}
	start := time.Now()
	report.StartedAt = start.UTC()
	run, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(run) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	loop := true
	for loop {
		select {
		case e := <-done:
			joined = true
			t.Errorf("worker stopped before reconciliation: %v", e)
			loop = false
		case <-time.After(20 * time.Millisecond):
			if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_heads WHERE tenant_id=$1 AND desired_revision=active_revision AND sync_status='running'`, f.tenant).Scan(&report.Resolved); e != nil {
				t.Error(e)
				loop = false
			}
			if report.Resolved == 1000 {
				loop = false
			}
		}
	}
	report.ElapsedNS = int64(time.Since(start))
	report.ElapsedMS = float64(report.ElapsedNS) / float64(time.Millisecond)
	report.EndedAt = time.Now().UTC()
	report.Observed = int(observed.Load())
	cancel()
	if !joined {
		<-done
		joined = true
	}
	if report.Resolved == 1000 && report.Observed == 2000 && bytes.Equal(before, inventory()) && bytes.Equal(authority, authoritySnapshot(t, f.pool, f.tenant, []string{"billing_snapshots", "package_snapshots", "traffic_sources", "ledger_events", "ledger_projections", "share_handoffs", "share_chains", "rule_revisions", "rule_compile_inputs", "rule_artifacts", "rule_artifact_commits"})) {
		report.Status = "passed"
	}
	// Every original socket and financial operation still retains occupation.
	var free int
	if e := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM operations WHERE tenant_id=$1 AND NOT occupation_held)+(SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state='released')`, f.tenant).Scan(&free); e != nil || free != 0 {
		report.Status = "failed"
		t.Error("reconciliation released occupation", e, free)
	}
	if report.Validate() != nil {
		report.Status = "failed"
	}
	raw, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	path := os.Getenv("PHASE6_RECONCILIATION_EVIDENCE")
	if path == "" {
		path = filepath.Join(t.TempDir(), "reconciliation_1000.json")
	}
	if e = os.WriteFile(path, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	t.Log(string(raw))
	if e = report.Validate(); e != nil {
		t.Fatal(e)
	}
}
