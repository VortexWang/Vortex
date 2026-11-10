package hub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestRecoveryAdmissionWirePinsBackendBootSessionRegionAndDigest(t *testing.T) {
	for _, name := range []string{"backend", "hub", "boot", "session", "region", "digest", "payload_backend", "payload_boot", "unknown_proto"} {
		t.Run(name, func(t *testing.T) {
			s, e := store.Open(t.TempDir()+"/hub.db", "hub")
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			c := &BackendClient{Store: s, BackendID: uuid.NewString(), HubID: uuid.NewString(), BootID: uuid.NewString(), Region: "admission"}
			now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
			p := store.HubRecoveryAdmission{Version: 1, RecoveryID: uuid.NewString(), BackendID: c.BackendID, HubID: c.HubID, BootID: c.BootID, FromEpoch: 7, ToEpoch: 8, SealedSequence: 1, AdmissionSHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), PrefixSHA256: strings.Repeat("c", 64), ObservedAt: now, AutonomyDeadline: now.Add(time.Hour)}
			raw, _ := json.Marshal(p)
			h := sha256.Sum256(raw)
			session := uuid.NewString()
			wire := &controlv1.HubRecoveryAdmission{BackendId: c.BackendID, HubId: c.HubID, BootId: c.BootID, RegionId: c.Region, SessionId: session, Payload: raw, PayloadSha256: h[:]}
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
				wire.RegionId = "other"
			case "digest":
				wire.PayloadSha256 = []byte("bad")
			case "payload_backend":
				p.BackendID = uuid.NewString()
			case "payload_boot":
				p.BootID = uuid.NewString()
			case "unknown_proto":
				wire.ProtoReflect().SetUnknown([]byte{0x40, 0x01})
			}
			if strings.HasPrefix(name, "payload_") {
				wire.Payload, _ = json.Marshal(p)
				h := sha256.Sum256(wire.Payload)
				wire.PayloadSha256 = h[:]
			}
			if e = c.applyRecoveryAdmission(context.Background(), session, wire); e == nil {
				t.Fatal("unbound admission wire accepted")
			}
			rows, e := s.PendingLedger(context.Background(), 1000)
			if e != nil || len(rows) != 0 {
				t.Fatal("invalid admission changed local journal", rows, e)
			}
		})
	}
}
