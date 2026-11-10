package agent

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestSourceControlFULLReplayBeforeClockAndStaleFirstInstall(t *testing.T) {
	c, f, _ := recoveryFenceControlFixture(t)
	p := store.SourceProvision{Version: 1, ID: uuid.NewString(), NodeID: c.Node.NodeID, BootID: c.Node.BootID, BackendID: c.SourceBackendID, IssuedAt: f.IssuedAt}
	for i := 0; i < 3; i++ {
		r := f.Root
		r.ID = uuid.NewString()
		r.SourceID = uuid.NewString()
		p.Roots = append(p.Roots, r)
	}
	raw, _ := json.Marshal(p)
	wire := &controlv1.SourceProvisionCommand{Payload: raw, PayloadSha256: sha256BytesControl(raw)}
	r, e := c.provisionSourcesFromBackend(context.Background(), c.SourceBackendID, wire)
	if e != nil {
		t.Fatal(e)
	}
	// A trusted conservative lower bound can still precede a freshly issued
	// command. Intersect its authenticated time before the first FULL install.
	at := time.Now()
	c.Node.bootStarted = at.Add(-time.Second)
	c.Node.clock = &AuthorityClock{now: time.Now}
	if e = c.Node.clock.observe(at.Add(-5*time.Millisecond), c.Node.bootStarted, at); e != nil {
		t.Fatal(e)
	}
	p.ID = uuid.NewString()
	p.IssuedAt = at.UTC()
	wire.Payload, _ = json.Marshal(p)
	wire.PayloadSha256 = sha256BytesControl(wire.Payload)
	r, e = c.provisionSourcesFromBackend(context.Background(), c.SourceBackendID, wire)
	if e != nil {
		t.Fatal("fresh command could not narrow trusted time", e)
	}
	c.Node.clock = &AuthorityClock{now: time.Now}
	c.Node.bootStarted = time.Now().Add(-time.Hour)
	replay, e := c.provisionSourcesFromBackend(context.Background(), c.SourceBackendID, wire)
	if e != nil || replay != r {
		t.Fatal("FULL replay required clock bootstrap", e)
	}
	p.ID = uuid.NewString()
	wire.Payload, _ = json.Marshal(p)
	wire.PayloadSha256 = sha256BytesControl(wire.Payload)
	if _, e = c.provisionSourcesFromBackend(context.Background(), c.SourceBackendID, wire); e == nil {
		t.Fatal("first install bypassed stale boot clock")
	}
}
