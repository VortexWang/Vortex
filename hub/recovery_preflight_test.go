package hub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	"path/filepath"
	"testing"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestRecoveryPreflightCommandBindsActualSessionBeforeFULL(t *testing.T) {
	for _, name := range []string{"backend", "hub", "boot", "session", "region", "digest", "payload backend", "payload boot", "unknown proto"} {
		t.Run(name, func(t *testing.T) {
			p := sameStoreCommandFixture()
			raw, _ := json.Marshal(p)
			hash := sha256.Sum256(raw)
			session := uuid.NewString()
			st, e := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
			if e != nil {
				t.Fatal(e)
			}
			defer st.Close()
			c := &BackendClient{Store: st, BackendID: p.BackendID, HubID: p.HubID, BootID: p.ToBootID, Region: "test"}
			wire := &controlv1.HubRecoveryCommand{BackendId: p.BackendID, HubId: p.HubID, BootId: p.ToBootID, SessionId: session, RegionId: c.Region, Payload: raw, PayloadSha256: hash[:]}
			switch name {
			case "backend":
				wire.BackendId = uuid.NewString()
			case "hub":
				wire.HubId = uuid.NewString()
			case "boot":
				wire.BootId = uuid.NewString()
			case "session":
				wire.SessionId = uuid.NewString()
			case "region":
				wire.RegionId = "elsewhere"
			case "digest":
				wire.PayloadSha256 = []byte("bad")
			case "payload backend":
				p.BackendID = uuid.NewString()
				p.Handoff.BackendID = p.BackendID
			case "payload boot":
				p.ToBootID = uuid.NewString()
			case "unknown proto":
				wire.ProtoReflect().SetUnknown([]byte{0x40, 0x01})
			}
			if name == "payload backend" || name == "payload boot" {
				wire.Payload, _ = json.Marshal(p)
				h := sha256.Sum256(wire.Payload)
				wire.PayloadSha256 = h[:]
			}
			if _, _, e = c.applyRecoveryPreflightCommand(context.Background(), session, wire); e == nil {
				t.Fatal("unbound recovery command accepted")
			}
			rows, e := st.PendingLedger(context.Background(), 1000)
			if e != nil || len(rows) != 0 {
				t.Fatal("rejected command wrote FULL journal", rows, e)
			}
		})
	}
}
