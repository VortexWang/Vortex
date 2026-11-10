package agent

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestProcessReplayCountCannotSubstituteReceiptIdentity(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE usage_batches(node_id TEXT,batch_id TEXT,PRIMARY KEY(node_id,batch_id));
CREATE TABLE usage_backend_receipts(node_id TEXT,batch_id TEXT,PRIMARY KEY(node_id,batch_id));
INSERT INTO usage_batches VALUES('node','a'),('node','b');
INSERT INTO usage_backend_receipts VALUES('node','a'),('other','b'),('node','unrelated');`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if drained, err := processBackendReceiptsDrained(ctx, db, 2); err != nil || drained {
		t.Fatalf("extra/foreign receipts substituted missing identity: drained=%v err=%v", drained, err)
	}
	if _, err := db.Exec(`INSERT INTO usage_backend_receipts VALUES('node','b')`); err != nil {
		t.Fatal(err)
	}
	if drained, err := processBackendReceiptsDrained(ctx, db, 2); err != nil || !drained {
		t.Fatalf("complete identities did not drain: drained=%v err=%v", drained, err)
	}
	if _, err := db.Exec(`DELETE FROM usage_backend_receipts WHERE batch_id IN ('a','b','unrelated')`); err != nil {
		t.Fatal(err)
	}
	if drained, err := processBackendReceiptsDrained(ctx, db, 2); err != nil || drained {
		t.Fatalf("missing receipts accepted: drained=%v err=%v", drained, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if drained, err := processBackendReceiptsDrained(cancelled, db, 2); err == nil || drained {
		t.Fatalf("cancelled audit accepted: drained=%v err=%v", drained, err)
	}
}

func TestProcessAgentDrainStillValidatesEveryStateAndPayload(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE usage_batches(payload BLOB,state TEXT); INSERT INTO usage_batches VALUES(X'', 'hub_acked');`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, state := range []string{"pending", "locally_acked", "ledgered", "invalid", "hub_acked"} {
		if _, err := db.Exec(`UPDATE usage_batches SET state=?`, state); err != nil {
			t.Fatal(err)
		}
		complete, err := processAgentUsageComplete(ctx, db, 0)
		if err != nil || complete != (state == "hub_acked") {
			t.Fatalf("state %q: complete=%v err=%v", state, complete, err)
		}
	}
	if _, err := db.Exec(`UPDATE usage_batches SET payload=X'ff'`); err != nil {
		t.Fatal(err)
	}
	if complete, err := processAgentUsageComplete(ctx, db, 0); complete || err == nil {
		t.Fatalf("malformed payload accepted: complete=%v err=%v", complete, err)
	}
	raw, err := proto.Marshal(&controlv1.UsageBatch{Deltas: []*controlv1.UsageDelta{{Bytes: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE usage_batches SET payload=?`, raw); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []uint64{6, 7, 8} {
		complete, err := processAgentUsageComplete(ctx, db, expected)
		if err != nil || complete != (expected == 7) {
			t.Fatalf("byte total %d: complete=%v err=%v", expected, complete, err)
		}
	}
}
