package phase5_test

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

func TestSharePolicyProcess(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s %v %s", name, err, out)
		}
	}
	for _, mode := range []string{"policy-cutover", "policy-future-revoked", "policy-hub-restart", "share-invalid-parent", "share-invalid-issuer", "share-invalid-cycle", "share-invalid-capacity", "share-invalid-revision"} {
		t.Run(mode, func(t *testing.T) { regionalTrafficContinuity(t, binaries, mode) })
	}
}

func preparePolicyProcess(t *testing.T, ctx context.Context, dir string, d store.HubDelegation, mode string) string {
	t.Helper()
	future := d
	future.ID = "550e8400-e29b-41d4-a716-446655441800"
	future.SourceID = "550e8400-e29b-41d4-a716-446655441801"
	future.RuntimeSourceID = "550e8400-e29b-41d4-a716-446655441802"
	future.ShareSourceID = "550e8400-e29b-41d4-a716-446655441803"
	future.BillingSnapshotID = "550e8400-e29b-41d4-a716-446655441804"
	future.PreissueSeconds = 86400
	future.ValidFrom = time.Now().UTC().Add(6 * time.Second)
	future.ShareReferences = append([]store.ShareReference(nil), d.ShareReferences...)
	for i := range future.ShareReferences {
		future.ShareReferences[i].ID = fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 1805+i)
		future.ShareReferences[i].SourceID = []string{future.SourceID, future.RuntimeSourceID, future.ShareSourceID}[i]
	}
	for _, entry := range []struct{ name, owner string }{{"agent.db", "agent"}, {"hub0.db", "hub"}} {
		s, err := store.Open(filepath.Join(dir, entry.name), entry.owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, root := range future.ShareReferences {
			if err = s.InstallShareSourceAnchor(ctx, d.NodeID, regionalBoot, root); err != nil {
				s.Close()
				t.Fatal(err)
			}
		}
		s.Close()
	}
	p := store.PolicyTransition{ID: "550e8400-e29b-41d4-a716-446655441810", RuleID: d.RuleID, PathGeneration: d.PathGeneration, Old: store.PolicySnapshot{ID: d.BillingSnapshotID, RuleID: d.RuleID, NodeID: d.NodeID, PathGeneration: d.PathGeneration, Revision: 1, CoefficientMilli: 1000}, New: store.PolicySnapshot{ID: future.BillingSnapshotID, RuleID: d.RuleID, NodeID: d.NodeID, PathGeneration: d.PathGeneration, Revision: 2, CoefficientMilli: 1500}, EffectiveAt: future.ValidFrom, PreissueSeconds: 86400, GuardSeconds: 1, FutureSourceID: future.SourceID}
	raw, _ := json.Marshal(struct {
		Delegation store.HubDelegation
		Transition store.PolicyTransition
	}{future, p})
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exercisePolicyProcess(t *testing.T, ctx context.Context, dir, mode string, db *sql.DB, cfg config.Config, mux *vlink.MuxLink, stream *vlink.MuxStream, exchange func(string), restart func()) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Delegation store.HubDelegation
		Transition store.PolicyTransition
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	p := fixture.Transition
	exchange("old-snapshot-before-effective")
	hash := sha256.Sum256([]byte(cfg.RuleID))
	var rule [16]byte
	copy(rule[:], hash[:16])
	open := func(network uint8) (*vlink.MuxStream, error) {
		s, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: network, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			return nil, err
		}
		err = s.WaitOpen(ctx)
		return s, err
	}
	udp, err := open(2)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Reset(ctx, 0)
	udpExchange := func(s *vlink.MuxStream, payload string) {
		t.Helper()
		accepted, err := s.WriteDgram(ctx, []byte(payload))
		if err != nil || !accepted {
			t.Fatalf("UDP send %v %v", accepted, err)
		}
		f, err := s.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Release()
		if f.Type != vlink.FrameDgram || string(f.Payload) != payload {
			t.Fatal("UDP data changed")
		}
		if err = s.Consume(ctx, uint32(len(f.Payload))); err != nil {
			t.Fatal(err)
		}
	}
	udpExchange(udp, "old-UDP")
	waitUntil := func(at time.Time) {
		t.Helper()
		delay := time.Until(at)
		if delay <= 0 {
			return
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("policy boundary timeout")
		case <-timer.C:
		}
	}
	waitUntil(p.EffectiveAt.Add(-500 * time.Millisecond))
	if s, err := open(1); err == nil {
		s.Reset(ctx, 0)
		t.Fatal("guard allowed new old-period TCP")
	}
	exchange("old-TCP-during-guard")
	udpExchange(udp, "old-UDP-during-guard")
	waitUntil(p.EffectiveAt.Add(300 * time.Millisecond))
	next, err := open(1)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Reset(ctx, 0)
	if err = next.WriteData(ctx, []byte("new-policy-TCP")); err != nil {
		t.Fatal(err)
	}
	frame, err := next.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame.Payload) != "new-policy-TCP" {
		t.Fatal("new policy TCP data changed")
	}
	frame.Release()
	nextUDP, err := open(2)
	if err != nil {
		t.Fatal(err)
	}
	defer nextUDP.Reset(ctx, 0)
	udpExchange(nextUDP, "new-policy-UDP")
	if mode == "policy-hub-restart" {
		restart()
	}
	exchange("old-TCP-after-effective")
	udpExchange(udp, "old-UDP-after-effective")
	var oldCount, newCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM runtime_sessions s JOIN authorization_records a ON a.node_id=s.node_id AND a.authorization_id=s.initial_grant_id WHERE a.billing_snapshot_id=?`, p.Old.ID).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM runtime_sessions s JOIN authorization_records a ON a.node_id=s.node_id AND a.authorization_id=s.initial_grant_id WHERE a.billing_snapshot_id=?`, p.New.ID).Scan(&newCount); err != nil || oldCount < 2 || newCount < 2 {
		t.Fatalf("snapshots old=%d new=%d %v", oldCount, newCount, err)
	}
	if mode == "policy-future-revoked" {
		var installed []byte
		if err = db.QueryRowContext(ctx, `SELECT payload FROM authorization_updates WHERE CAST(payload AS TEXT) LIKE '%policy_transitions%' ORDER BY rowid DESC LIMIT 1`).Scan(&installed); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err = json.Unmarshal(installed, &payload); err != nil {
			t.Fatal(err)
		}
		payload["update_id"] = "550e8400-e29b-41d4-a716-446655441999"
		payload["authorizations"] = []any{}
		payload["share_references"] = []any{}
		payload["policy_transitions"] = []any{}
		payload["revocations"] = []string{p.ID}
		raw, _ := json.Marshal(payload)
		wire, _ := json.Marshal(&controlv1.AuthorizationUpdate{Generation: cfg.PathGeneration, Payload: raw})
		if err = os.WriteFile(filepath.Join(dir, "policy-refresh.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
		for {
			var n int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM authorization_revocations WHERE authorization_id=?`, p.ID).Scan(&n); err == nil && n == 1 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("policy revocation not installed")
			case <-time.After(20 * time.Millisecond):
			}
		}
		if s, err := open(1); err == nil {
			s.Reset(ctx, 0)
			t.Fatal("revoked policy admitted TCP")
		}
		if s, err := open(2); err == nil {
			s.Reset(ctx, 0)
			t.Fatal("revoked policy admitted UDP")
		}
		// Replaying the exact old successful update cannot undo a later tombstone.
		late, _ := json.Marshal(&controlv1.AuthorizationUpdate{Generation: cfg.PathGeneration, Payload: installed})
		if err = os.WriteFile(filepath.Join(dir, "policy-refresh.json"), late, 0600); err != nil {
			t.Fatal(err)
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("late replay wait")
		case <-timer.C:
		}
		if s, err := open(1); err == nil {
			s.Reset(ctx, 0)
			t.Fatal("late success revived revoked policy")
		}
		exchange("existing-old-after-policy-revocation")
		udpExchange(nextUDP, "existing-new-after-policy-revocation")
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM config_share_holds WHERE state='held'`).Scan(&n); err != nil || n < 3 {
		t.Fatalf("no config share occupancy %d %v", n, err)
	}
	retainPolicyLedgers(t, dir, mode, db)
	var upper, state string
	if err = db.QueryRowContext(ctx, `SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='command-debt'`).Scan(&upper, &state); err != nil || upper != "512" || state != "unresolved" {
		t.Fatal("policy changed unknown liability", upper, state, err)
	}
}

func retainPolicyLedgers(t *testing.T, dir, mode string, nodeDB *sql.DB) {
	t.Helper()
	out := os.Getenv("PHASE5_POLICY_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	modes := map[string]string{}
	for _, entry := range []struct {
		name string
		db   *sql.DB
	}{{"agent", nodeDB}, {"hub", nil}} {
		db := entry.db
		if db == nil {
			var err error
			db, err = openProcessLedger(filepath.Join(dir, "hub0.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
		}
		var journal string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
			t.Fatal("source is not WAL", journal, err)
		}
		modes[entry.name] = journal
		path := filepath.Join(t.TempDir(), "snapshot.db")
		if _, err := db.Exec("VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'"); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		hashes[entry.name] = hex.EncodeToString(sum[:])
		f, err := os.Create(filepath.Join(out, mode+"-"+entry.name+".sqlite.gz"))
		if err != nil {
			t.Fatal(err)
		}
		z := gzip.NewWriter(f)
		if _, err = z.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]any{"hashes": hashes, "source_journal_modes": modes, "snapshot_method": "VACUUM INTO; standalone backup may use delete journal"})
	if err := os.WriteFile(filepath.Join(out, mode+"-ledgers.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func exerciseInvalidShareProcess(t *testing.T, ctx context.Context, db *sql.DB, hubPath string, peer, cfg config.Config, targetCount func() int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	supervisor, err := link.New(peer)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.TLS.ServerName = cfg.NodeID
	defer supervisor.Close()
	ready := make(chan *vlink.MuxLink, 1)
	go func() {
		for ctx.Err() == nil {
			_ = supervisor.Dial(ctx, func(c context.Context, m *vlink.MuxLink, _ vlink.NodeID) error {
				select {
				case ready <- m:
				case <-c.Done():
					return c.Err()
				}
				<-c.Done()
				return c.Err()
			})
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	var mux *vlink.MuxLink
	select {
	case mux = <-ready:
	case <-ctx.Done():
		t.Fatal("actual denied OPEN did not reach TLS/HELLO boundary", ctx.Err())
	}
	hash := sha256.Sum256([]byte(cfg.RuleID))
	var rule [16]byte
	copy(rule[:], hash[:16])
	for _, network := range []uint8{1, 2} {
		s, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: network, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.WaitOpen(ctx); err == nil {
			t.Fatal("invalid share reached actual OPEN")
		}
	}
	if targetCount() != 0 {
		t.Fatal("invalid share reached TCP target")
	}
	var active, grants int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM config_results WHERE state='active'`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("invalid share allowed Commit %d %v", active, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM authorization_records`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("invalid share installed grants %d %v", grants, err)
	}
	hubDB, err := openProcessLedger(hubPath)
	if err != nil {
		t.Fatal(err)
	}
	defer hubDB.Close()
	if err = hubDB.QueryRowContext(ctx, `SELECT count(*) FROM issuer_issues`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("invalid share minted rights %d %v", grants, err)
	}
}
