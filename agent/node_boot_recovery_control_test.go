package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func bootRecoveryControlFixture(t *testing.T) (*ControlClient, contracts.NodeBootRecovery, *controlv1.NodeBootRecoveryCommand) {
	t.Helper()
	st, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	n, e := New(uuid.NewString(), st, nil)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	c := contracts.NodeBootRecovery{Version: 1, ID: uuid.NewString(), BackendID: creditCloseBackendID, TenantID: uuid.NewString(), NodeID: n.NodeID, FromBootID: uuid.NewString(), ToBootID: n.BootID, MinimumFencing: 4, SafeAfter: at.Add(-time.Minute), IssuedAt: at, ExpiresAt: at.Add(time.Minute), AuthorityProofSHA256: strings.Repeat("a", 64)}
	for _, kind := range []string{"rate", "connections"} {
		c.Previous = append(c.Previous, contracts.ShareHandoff{TenantID: c.TenantID, ID: uuid.NewString(), NodeID: c.NodeID, BootID: c.FromBootID, Kind: kind, SignerID: uuid.NewString(), SnapshotID: uuid.NewString(), RuleID: uuid.NewString(), RuleRevision: 1, Generation: 1, Predecessor: 0, SignerEpoch: 7, PathGeneration: 1, FencingGeneration: 3, Amount: 1, IssuedAt: at.Add(-2 * time.Minute), OwnershipUntil: c.SafeAfter, PolicyUntil: c.SafeAfter, AutonomyUntil: c.SafeAfter})
	}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = contracts.DecodeNodeBootRecovery(raw); e != nil {
		t.Fatal(e)
	}
	return &ControlClient{Node: n, SourceBackendID: c.BackendID}, c, &controlv1.NodeBootRecoveryCommand{Payload: raw, PayloadSha256: sha256BytesControl(raw)}
}
func TestNodeBootRecoveryControlPinsScopeBeforeFULLAndReplaysWithoutClock(t *testing.T) {
	for _, name := range []string{"unpinned", "wrong issuer", "wrong backend", "wrong node", "wrong boot", "digest", "unknown proto"} {
		t.Run(name, func(t *testing.T) {
			client, c, wire := bootRecoveryControlFixture(t)
			issuer := c.BackendID
			switch name {
			case "unpinned":
				client.SourceBackendID = ""
			case "wrong issuer":
				issuer = uuid.NewString()
			case "wrong backend":
				c.BackendID = uuid.NewString()
			case "wrong node":
				c.NodeID = uuid.NewString()
			case "wrong boot":
				c.ToBootID = uuid.NewString()
			case "digest":
				wire.PayloadSha256 = []byte("wrong")
			case "unknown proto":
				wire.ProtoReflect().SetUnknown([]byte{0x18, 0x01})
			}
			if name == "wrong backend" || name == "wrong node" || name == "wrong boot" {
				wire.Payload, _ = json.Marshal(c)
				wire.PayloadSha256 = sha256BytesControl(wire.Payload)
			}
			if _, e := client.recoverNodeBootFromBackend(context.Background(), issuer, wire); e == nil {
				t.Fatal("invalid command installed")
			}
		})
	}
	client, c, wire := bootRecoveryControlFixture(t)
	first, e := client.recoverNodeBootFromBackend(context.Background(), c.BackendID, wire)
	if e != nil {
		t.Fatal(e)
	}
	// Receipt replay must not require reanchoring a stale boot clock, and cannot
	// extend a different first-install command after its immutable expiry.
	client.Node.clock = &AuthorityClock{now: time.Now}
	client.Node.bootStarted = time.Now().Add(-time.Hour)
	again, e := client.recoverNodeBootFromBackend(context.Background(), c.BackendID, wire)
	if e != nil || !bytes.Equal(again.Payload, first.Payload) {
		t.Fatal("FULL replay depends on live clock", e)
	}
	changed := c
	changed.ID = uuid.NewString()
	changed.IssuedAt = c.IssuedAt.Add(-time.Hour)
	changed.ExpiresAt = changed.IssuedAt.Add(time.Minute)
	changed.SafeAfter = changed.IssuedAt
	for i := range changed.Previous {
		changed.Previous[i].IssuedAt = changed.IssuedAt.Add(-2 * time.Minute)
		changed.Previous[i].OwnershipUntil = changed.SafeAfter
		changed.Previous[i].PolicyUntil = changed.SafeAfter
		changed.Previous[i].AutonomyUntil = changed.SafeAfter
	}
	raw, _ := json.Marshal(changed)
	if _, e = client.recoverNodeBootFromBackend(context.Background(), c.BackendID, &controlv1.NodeBootRecoveryCommand{Payload: raw, PayloadSha256: sha256BytesControl(raw)}); e == nil {
		t.Fatal("stale first install bootstrapped clock")
	}
}
