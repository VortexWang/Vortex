package config

import (
	"encoding/json"
	"testing"
	"vortex/engine/vlink"
)

func TestTransportBoundsRejectUnsupportedAndUnboundedModes(t *testing.T) {
	for _, tc := range []struct {
		mode     vlink.TransportMode
		min, max int
		valid    bool
	}{
		{"", 0, 0, true}, {"", 1, 4, false}, {"auto", 1, 4, false}, {"mux", 1, 4, true},
		{"pool", 0, 0, true}, {"pool", 5, 4, false}, {"pool", 1, 17, false}, {"pool", -1, 4, false},
	} {
		var cfg Config
		if err := json.Unmarshal([]byte(validJSON()), &cfg); err != nil {
			t.Fatal(err)
		}
		cfg.TransportMode, cfg.PoolMinLinks, cfg.PoolMaxLinks = tc.mode, tc.min, tc.max
		if err := cfg.Validate(); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
