package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsExplicitFreshDirectory(t *testing.T) {
	stop, err := startDiagnostics("")
	if err != nil {
		t.Fatal(err)
	}
	stop()
	dir := filepath.Join(t.TempDir(), "profiles")
	stop, err = startDiagnostics(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startDiagnostics(dir); err == nil {
		t.Fatal("existing evidence overwritten")
	}
	stop()
	for _, name := range []string{"cpu", "block", "mutex"} {
		info, err := os.Stat(filepath.Join(dir, name+".pprof"))
		if err != nil || info.Size() == 0 {
			t.Fatalf("missing profile %s %v", name, err)
		}
	}
}
