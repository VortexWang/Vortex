package link

import (
	"testing"

	"vortex/engine/runtime/config"
)

func TestSupervisorRequiresConfiguredIdentity(t *testing.T) {
	if _, err := New(config.Config{}); err == nil {
		t.Fatal("expected missing certificate failure")
	}
}
