package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/accounting"
)

const creditCloseBackendID = "550e8400-e29b-41d4-a716-000000000999"

type creditCloseTestBackend struct {
	controlv1.UnimplementedNodeControlServer
	command  *controlv1.CreditCloseCommand
	drop     bool
	wantCap  bool
	received chan contracts.CreditCloseReceipt
}

func (b *creditCloseTestBackend) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	hasCap := false
	for _, cap := range hello.GetHeartbeat().GetCapabilities() {
		if cap == "credit-close-v1" {
			hasCap = true
		}
	}
	if hasCap != b.wantCap {
		return fmt.Errorf("unexpected close capability: %t", hasCap)
	}
	if err := stream.Send(&controlv1.ControlMessage{MessageId: "550e8400-e29b-41d4-a716-000000000998", Sequence: 1, Body: &controlv1.ControlMessage_CreditCloseCommand{CreditCloseCommand: b.command}}); err != nil {
		return err
	}
	message, err := stream.Recv()
	if err != nil {
		return err
	}
	wire := message.GetCreditCloseReceipt()
	if wire == nil || message.NodeId != hello.NodeId || message.BootId != hello.BootId || message.Sequence <= hello.Sequence || !bytes.Equal(wire.PayloadSha256, sha256BytesControl(wire.Payload)) {
		return errors.New("invalid close receipt identity/sequence/hash")
	}
	r, err := contracts.DecodeCreditCloseReceipt(wire.Payload)
	if err != nil {
		return err
	}
	c, err := contracts.DecodeCreditCloseCommand(b.command.Payload)
	if err != nil || r.CommandID != c.ID || r.BackendID != c.BackendID || r.NodeID != c.NodeID || r.BootID != c.BootID || r.GrantID != c.GrantID || r.CommandSHA256 != hex.EncodeToString(b.command.PayloadSha256) {
		return errors.New("close receipt does not match original command")
	}
	if b.drop {
		return status.Error(codes.Unavailable, "drop Node FULL receipt before backend acceptance")
	}
	b.received <- r
	return nil
}

func startCreditCloseTestBackend(t *testing.T, server *creditCloseTestBackend, config *tls.Config) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer(grpc.Creds(credentials.NewTLS(config)))
	controlv1.RegisterNodeControlServer(g, server)
	go func() { _ = g.Serve(listener) }()
	t.Cleanup(func() { g.Stop(); _ = listener.Close() })
	return listener
}

func creditCloseWire(t *testing.T, f *renewalSafetyFixture, grant string) *controlv1.CreditCloseCommand {
	t.Helper()
	c := contracts.CreditCloseCommand{Version: 1, ID: "550e8400-e29b-41d4-a716-000000000997", BackendID: creditCloseBackendID, HubID: f.delegation.HubID, NodeID: f.node.NodeID, BootID: f.node.BootID, GrantID: grant, Generation: 1, Original: f.result.AuthorizationUpdate.Payload}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return &controlv1.CreditCloseCommand{Payload: raw, PayloadSha256: sha256BytesControl(raw)}
}

func closeOverTLS(t *testing.T, f *renewalSafetyFixture, command *controlv1.CreditCloseCommand, drop, pinned bool) (contracts.CreditCloseReceipt, error) {
	t.Helper()
	serverTLS, clientTLS, _ := controlTLSWithHub(t, f.node.NodeID, creditCloseBackendID)
	b := &creditCloseTestBackend{command: command, drop: drop, wantCap: pinned, received: make(chan contracts.CreditCloseReceipt, 1)}
	l := startCreditCloseTestBackend(t, b, serverTLS)
	c := &ControlClient{Node: f.node, TLS: clientTLS}
	if pinned {
		c.SourceBackendID = creditCloseBackendID
		c.ControlOnly = true
	}
	err := c.Connect(f.ctx, l.Addr().String())
	select {
	case r := <-b.received:
		return r, err
	default:
		return contracts.CreditCloseReceipt{}, err
	}
}

func TestCreditCloseActualMTLSLostReceiptReplaysNeverInstalledWitness(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	command := creditCloseWire(t, f, grant)
	if _, err := closeOverTLS(t, f, command, true, true); err == nil {
		t.Fatal("lost receipt reported backend success")
	}
	local, err := f.agent.FinalizeStoppedCreditGrant(f.ctx, f.node.NodeID, f.node.BootID, f.delegation.HubID, grant)
	if err != nil || !local.NeverInstalled {
		t.Fatalf("lost receipt lost FULL witness: %+v %v", local, err)
	}
	r, err := closeOverTLS(t, f, command, false, true)
	if err != nil || r.State != "final" || r.CommandID == "" {
		t.Fatalf("retry receipt=%+v %v", r, err)
	}
	var final store.CreditGrantFinal
	if json.Unmarshal(r.Final, &final) != nil || final != local {
		t.Fatalf("reconnect changed immutable witness: %s", r.Final)
	}
}

func TestCreditCloseActualMTLSPendingWriteCannotProduceFinal(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	if err := f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result); err != nil {
		t.Fatal(err)
	}
	p, err := validateAuthorizationUpdate(f.result.AuthorizationUpdate.Payload, f.node.NodeID, f.node.BootID, 1)
	if err != nil {
		t.Fatal(err)
	}
	instant := p.IssuedAt.Add(time.Millisecond)
	gate, err := f.node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: grant, RuleID: f.delegation.RuleID, RuleRevision: 1, PathGeneration: 1, SnapshotID: f.delegation.BillingSnapshotID, Clock: func() (time.Time, time.Time, bool) { return instant, instant, true }})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(f.ctx, accounting.Flow{Key: "pending-wire-close", RuleID: f.delegation.RuleID})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	batch, err := session.Reserve(f.ctx, accounting.LeftToRight, 64)
	if err != nil {
		t.Fatal(err)
	}
	command := creditCloseWire(t, f, grant)
	r, err := closeOverTLS(t, f, command, false, true)
	if err != nil || r.State != "unsettled" || len(r.Final) != 0 {
		t.Fatalf("writable ticket finalized: %+v %v", r, err)
	}
	if err := batch.Commit(f.ctx, 37); err != nil {
		t.Fatal(err)
	}
	r, err = closeOverTLS(t, f, command, false, true)
	var final store.CreditGrantFinal
	if err != nil || r.State != "final" || json.Unmarshal(r.Final, &final) != nil || final.NeverInstalled || final.Used != 37 || final.Unused != 4059 {
		t.Fatalf("settled retry=%+v %+v %v", r, final, err)
	}
}

func TestCreditCloseActualMTLSRequiresExplicitPinnedBackend(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	command := creditCloseWire(t, f, grant)
	if r, err := closeOverTLS(t, f, command, false, false); err == nil || r.State != "" {
		t.Fatalf("unnegotiated command accepted: %+v %v", r, err)
	}
	if _, err := f.agent.FinalizeStoppedCreditGrant(f.ctx, f.node.NodeID, f.node.BootID, f.delegation.HubID, grant); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("unnegotiated command created stop/final: %v", err)
	}
	// Exact TLS role identity is checked before opening a control stream.
	serverTLS, clientTLS, _ := controlTLSWithHub(t, f.node.NodeID, creditCloseBackendID)
	b := &creditCloseTestBackend{command: command, wantCap: true, received: make(chan contracts.CreditCloseReceipt, 1)}
	l := startCreditCloseTestBackend(t, b, serverTLS)
	ctx, cancel := context.WithTimeout(f.ctx, 250*time.Millisecond)
	defer cancel()
	c := &ControlClient{Node: f.node, TLS: clientTLS, ControlOnly: true, SourceBackendID: "550e8400-e29b-41d4-a716-000000000996"}
	if err := c.Connect(ctx, l.Addr().String()); err == nil {
		t.Fatal("different valid TLS backend identity accepted")
	}
}

func TestCreditCloseActualMTLSRejectsWrongScopeAndDigestBeforeStop(t *testing.T) {
	for _, mutation := range []string{"boot", "backend", "grant", "digest"} {
		t.Run(mutation, func(t *testing.T) {
			f := newRenewalSafetyFixture(t)
			grant := issuedClosureGrant(t, f)
			wire := creditCloseWire(t, f, grant)
			c, err := contracts.DecodeCreditCloseCommand(wire.Payload)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "boot":
				c.BootID = "550e8400-e29b-41d4-a716-000000000995"
			case "backend":
				c.BackendID = "550e8400-e29b-41d4-a716-000000000995"
			case "grant":
				c.GrantID = "550e8400-e29b-41d4-a716-000000000995"
			}
			wire.Payload, err = json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			wire.PayloadSha256 = sha256BytesControl(wire.Payload)
			if mutation == "digest" {
				wire.PayloadSha256[0] ^= 255
			}
			if r, err := closeOverTLS(t, f, wire, false, true); err == nil || r.State != "" {
				t.Fatalf("invalid command produced receipt: %+v %v", r, err)
			}
			if _, err := f.agent.FinalizeStoppedCreditGrant(f.ctx, f.node.NodeID, f.node.BootID, f.delegation.HubID, grant); !errors.Is(err, store.ErrAuthorizationDenied) {
				t.Fatalf("invalid command changed original grant: %v", err)
			}
		})
	}
}
