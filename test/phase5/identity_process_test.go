package phase5_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// Real shipped node/Hub processes, bounded fixture CA enrollment authority,
// existing funded TCP and a fresh full TLS/exporter HELLO after live rotation.
// The CA/token issuer remains a Phase 5 protocol fixture, not Phase 6 authority.
func TestIdentityProcessRotation(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	regionalTrafficContinuity(t, binaries, "identity-rotation")
}
