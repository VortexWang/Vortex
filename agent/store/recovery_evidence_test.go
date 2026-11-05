package store

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func saveRecoveryLedger(t *testing.T, s *Store, path string) {
	t.Helper()
	out := os.Getenv("PHASE5_RECOVERY_EVIDENCE_DIR")
	if out == "" {
		return
	}
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint string
	if err = s.db.QueryRow(`SELECT value FROM agent_state WHERE key='ledger_checkpoint'`).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	var records, transfers int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM hub_ledger`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM hub_transfers`).Scan(&transfers); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if err = os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(out, "recovery-hub.sqlite.gz"))
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(file)
	if _, err = z.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	metadata := struct {
		Info               Info
		SHA256             string
		Checkpoint         string
		Records, Transfers int
		Scope              string
	}{info, hex.EncodeToString(sum[:]), checkpoint, records, transfers, "protocol-test-only"}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, "recovery-hub.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
