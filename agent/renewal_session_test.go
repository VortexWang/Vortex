package agent

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	controlv1 "vortex/contracts/protobuf/v1"
)

func renewalScriptListener(t *testing.T, config *tls.Config, run func(controlv1.NodeControl_ConnectServer) error) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.Creds(credentials.NewTLS(config)))
	controlv1.RegisterNodeControlServer(rpc, &regionalScriptServer{run: run})
	go rpc.Serve(ln)
	t.Cleanup(func() { rpc.Stop(); ln.Close() })
	return ln
}

func TestRenewalSafetyLateOldMutualTLSResultCannotInstall(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	session := f.consumeAndHold(t)
	defer session.Close()
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	serverTLS, clientTLS, _ := controlTLSWithHub(t, f.node.NodeID, f.delegation.HubID)
	f.client.TLS = clientTLS
	ready, late, attempted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	helloStatus := func(stream controlv1.NodeControl_ConnectServer) (string, error) {
		if _, err := stream.Recv(); err != nil {
			return "", err
		}
		hello, err := stream.Recv()
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		id, _ := newControlID()
		session := hello.GetLifecycleHello().GetSessionId()
		err = stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: 1, Body: &controlv1.ControlMessage_HubStatus{HubStatus: &controlv1.HubStatus{HubId: f.delegation.HubID, RegionId: "east", Mode: "online", SessionId: session, ControlVersion: "1.3.0", Capabilities: []string{"status-v1", "renewal-v1"}, SignerEpoch: 1, BackendHeartbeatAt: now.Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}}})
		return session, err
	}
	old := renewalScriptListener(t, serverTLS, func(stream controlv1.NodeControl_ConnectServer) error {
		if _, err := helloStatus(stream); err != nil {
			return err
		}
		for {
			message, err := stream.Recv()
			if err != nil {
				return err
			}
			if request := message.GetRenewalRequest(); request != nil {
				if request.RequestId != f.request.RequestId {
					return context.Canceled
				}
				break
			}
		}
		close(ready)
		select {
		case <-late:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer close(attempted)
		id, _ := newControlID()
		return stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: 2, Body: &controlv1.ControlMessage_RenewalResult{RenewalResult: f.result}})
	})
	successorSession := make(chan string, 1)
	successor := renewalScriptListener(t, serverTLS, func(stream controlv1.NodeControl_ConnectServer) error {
		session, err := helloStatus(stream)
		if err != nil {
			return err
		}
		successorSession <- session
		for {
			if _, err := stream.Recv(); err != nil {
				return err
			}
		}
	})
	// Cancel handlers before grpc.Stop on assertion failure, including the old
	// server's intentional wait after its transport context is already canceled.
	t.Cleanup(cancel)
	regional := &RegionalConfig{Region: "east", DialTimeout: time.Second, Watchdog: 2 * time.Second, FailoverBudget: 2 * time.Second, Renewal: &RenewalConfig{Retry: time.Second}}
	endpoint := func(address string) HubEndpoint {
		return HubEndpoint{Address: address, HubID: f.delegation.HubID, ServerName: f.delegation.HubID}
	}
	oldDone, newDone := make(chan error, 1), make(chan error, 1)
	f.node.clock.mu.Lock()
	anchored := f.node.clock.received
	f.node.clock.mu.Unlock()
	go func() { oldDone <- f.client.connect(ctx, endpoint(old.Addr().String()), regional) }()
	select {
	case <-ready:
	case err := <-oldDone:
		t.Fatalf("old stream stopped before request: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() { newDone <- f.client.connect(ctx, endpoint(successor.Addr().String()), regional) }()
	var current string
	select {
	case current = <-successorSession:
	case err := <-newDone:
		t.Fatalf("successor stopped: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		status := f.client.CurrentHubStatus()
		if status != nil && status.SessionId == current {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	close(late)
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal("old server never attempted late result")
	}
	select {
	case <-oldDone:
	case <-ctx.Done():
		t.Fatal("old receiver did not join")
	}
	intent, err := f.agent.RenewalIntent(f.ctx, f.request.RequestId)
	if err != nil || !sameRenewalIntent(intent, f.request) {
		t.Fatalf("late result changed durable request: %+v %v", intent, err)
	}
	f.node.clock.mu.Lock()
	after := f.node.clock.received
	f.node.clock.mu.Unlock()
	if !after.Equal(anchored) {
		t.Fatal("late result advanced authority time")
	}
	f.assertBalance(t, 4096) // Hub's unseen 4096 grant stays issued, not refunded.
	cancel()
	select {
	case <-newDone:
	case <-time.After(time.Second):
		t.Fatal("successor did not join")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	f.save(t, "renewal-safety-late-session")
}
