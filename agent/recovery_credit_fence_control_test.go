package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func recoveryFenceControlFixture(t *testing.T) (*ControlClient, store.RecoveryCreditFence, *controlv1.RecoveryCreditFenceCommand) {
	t.Helper()
	client, _, _ := bootRecoveryControlFixture(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	root := store.ShareReference{ID: uuid.NewString(), SourceID: uuid.NewString(), IssuerID: uuid.NewString(), NodeID: client.Node.NodeID, SignerEpoch: 7, Generation: 1, Revision: 1, RuleID: uuid.NewString(), RuleRevision: 1, PathGeneration: 1, RateBPS: 1000, Connections: 10, NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(5 * time.Minute), HardDeadline: at.Add(5 * time.Minute)}
	if e := client.Node.Store.InstallShareSourceAnchor(context.Background(), root.NodeID, client.SourceBackendID, root); e != nil {
		t.Fatal(e)
	}
	p := store.RecoveryCreditFence{Version: 1, ID: uuid.NewString(), RecoveryID: uuid.NewString(), BackendID: client.SourceBackendID, HubID: root.IssuerID, NodeID: root.NodeID, BootID: client.Node.BootID, ParentID: uuid.NewString(), SnapshotID: uuid.NewString(), ManifestSHA256: strings.Repeat("a", 64), SealedSequence: 1, IssuedAt: at, ExpiresAt: at.Add(time.Minute), FromEpoch: 7, ToEpoch: 8, OriginalRaw: 1024, Remaining: 1024, Root: root, Credits: []store.RecoveryCreditEntry{}}
	raw, _ := json.Marshal(p)
	return client, p, &controlv1.RecoveryCreditFenceCommand{Payload: raw, PayloadSha256: sha256BytesControl(raw)}
}

func TestRecoveryCreditFenceControlPinsIdentityAndFULLReplayBeforeClock(t *testing.T) {
	for _, name := range []string{"unpinned", "issuer", "backend", "node", "boot", "digest", "unknown proto"} {
		t.Run(name, func(t *testing.T) {
			c, p, wire := recoveryFenceControlFixture(t)
			issuer := c.SourceBackendID
			switch name {
			case "unpinned":
				c.SourceBackendID = ""
			case "issuer":
				issuer = uuid.NewString()
			case "backend":
				p.BackendID = uuid.NewString()
			case "node":
				p.NodeID = uuid.NewString()
			case "boot":
				p.BootID = uuid.NewString()
			case "digest":
				wire.PayloadSha256 = []byte("bad")
			case "unknown proto":
				wire.ProtoReflect().SetUnknown([]byte{0x18, 0x01})
			}
			if name == "backend" || name == "node" || name == "boot" {
				wire.Payload, _ = json.Marshal(p)
				wire.PayloadSha256 = sha256BytesControl(wire.Payload)
			}
			if _, e := c.fenceRecoveryCreditFromBackend(context.Background(), issuer, wire); e == nil {
				t.Fatal("invalid fence installed")
			}
		})
	}
	c, p, wire := recoveryFenceControlFixture(t)
	first, e := c.fenceRecoveryCreditFromBackend(context.Background(), p.BackendID, wire)
	if e != nil {
		t.Fatal(e)
	}
	c.Node.clock = &AuthorityClock{now: time.Now}
	c.Node.bootStarted = time.Now().Add(-time.Hour)
	again, e := c.fenceRecoveryCreditFromBackend(context.Background(), p.BackendID, wire)
	if e != nil || !bytes.Equal(first.Payload, again.Payload) {
		t.Fatal("FULL replay requires live clock", e)
	}
	p.ID = uuid.NewString()
	wire.Payload, _ = json.Marshal(p)
	wire.PayloadSha256 = sha256BytesControl(wire.Payload)
	if _, e = c.fenceRecoveryCreditFromBackend(context.Background(), p.BackendID, wire); e == nil {
		t.Fatal("new fence bypassed stale boot clock")
	}
}
