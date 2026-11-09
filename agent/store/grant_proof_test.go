package store

import (
	"bytes"
	"context"
	"testing"
)

func TestGrantProofRequiresSealAndAllPendingBatchesResolved(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	key := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	if _, _, err := s.PrepareGrantSettlementProof(ctx, key, "grant"); err == nil {
		t.Fatal("unsealed grant produced proof")
	}
	use := scope
	use.Kind = "credit_grant"
	use.AuthorizationID = "grant"
	use.BillingSnapshotID = "snapshot"
	known, unknown := runtimeReservation("known", 100), runtimeReservation("unknown", 200)
	for _, r := range []UsageReservation{known, unknown} {
		if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SealIdleGrant(ctx, key, "grant"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PrepareGrantSettlementProof(ctx, key, "grant"); err == nil {
		t.Fatal("pending writes counted as unused")
	}
	if err := s.SettleUsageReservation(ctx, known, "committed", 40, []byte("actual"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, unknown, "unknown", 200, []byte("unknown"), 200); err != nil {
		t.Fatal(err)
	}
	proof, digest, err := s.PrepareGrantSettlementProof(ctx, key, "grant")
	if err != nil || proof.Initial != 1<<20 || proof.Used != 40 || proof.Unknown != 200 || proof.Unused != (1<<20)-240 || len(digest) != 32 {
		t.Fatalf("bad final ledger: %+v %x %v", proof, digest, err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replay, hash, err := reopened.PrepareGrantSettlementProof(ctx, key, "grant")
	if err != nil || replay != proof || !bytes.Equal(digest, hash) {
		t.Fatalf("replay changed: %+v %v", replay, err)
	}
	balance, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Unknown != 200 || balance.Remaining != proof.Unused {
		t.Fatalf("proof released liabilities: %+v %v", balance, err)
	}
	if _, err := reopened.db.Exec(`UPDATE grant_settlement_proofs SET payload_sha256=X'00'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.PrepareGrantSettlementProof(ctx, key, "grant"); err == nil {
		t.Fatal("corrupt proof replayed")
	}
}

func TestGrantProofFailureDoesNotReturnOrReleaseCredit(t *testing.T) {
	s, _, _ := runtimeStore(t)
	ctx := context.Background()
	key := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	if err := s.SealIdleGrant(ctx, key, "grant"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_proof BEFORE INSERT ON grant_settlement_proofs BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	proof, hash, err := s.PrepareGrantSettlementProof(ctx, key, "grant")
	if err == nil || proof != (GrantSettlementProof{}) || hash != nil {
		t.Fatalf("failed write exposed proof: %+v %x %v", proof, hash, err)
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Remaining != balance.Initial {
		t.Fatalf("failed proof changed funding: %+v %v", balance, err)
	}
}
