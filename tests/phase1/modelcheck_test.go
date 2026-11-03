package phase1

import (
	"os"
	"path/filepath"
	"testing"

	modelcheck "vortex/tools/phase1"
)

func TestModelReport(t *testing.T) {
	root := t.TempDir()
	modelPath := filepath.Join(root, "model.go")
	if err := os.WriteFile(modelPath, []byte("package model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := modelcheck.Run(modelPath, filepath.Join(root, "results"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.CommitSHA == "" || report.ModelSHA256 == "" || report.GoToolchain == "" {
		t.Fatalf("invalid report: %+v", report)
	}
	if report.ImplementationStatus != "not-yet-implemented" {
		t.Fatal("implementation status was not explicit")
	}
	if _, err := os.Stat(filepath.Join(root, "results", "model-report.json")); err != nil {
		t.Fatal(err)
	}
}
