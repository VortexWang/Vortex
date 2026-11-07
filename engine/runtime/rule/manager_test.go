package rule

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
	"vortex/engine/accounting"
	"vortex/engine/apply"
)

type testGate struct {
	opens atomic.Int64
	bytes atomic.Int64
}

func (g *testGate) Open(context.Context, accounting.Flow) (accounting.Session, error) {
	g.opens.Add(1)
	return testSession{g}, nil
}

type testSession struct{ gate *testGate }

func (s testSession) Reserve(_ context.Context, _ accounting.Direction, n int64) (*accounting.Batch, error) {
	return accounting.NewBatch(n, func(_ context.Context, o accounting.Outcome) error { s.gate.bytes.Add(o.Used); return nil })
}
func (testSession) Close() error { return nil }
func localIP(t *testing.T) string {
	t.Helper()
	as, e := net.InterfaceAddrs()
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range as {
		if ip, _, e := net.ParseCIDR(a.String()); e == nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}
	t.Fatal("nonloopback local address required")
	return ""
}
func echoTarget(t *testing.T, prefix string) string {
	t.Helper()
	l, e := net.Listen("tcp", net.JoinHostPort(localIP(t), "0"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				b := make([]byte, 64)
				for {
					n, e := c.Read(b)
					if n > 0 {
						c.Write(append([]byte(prefix), b[:n]...))
					}
					if e != nil {
						return
					}
				}
			}()
		}
	}()
	return l.Addr().String()
}
func freePort(t *testing.T) int32 {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := int32(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return p
}
func artifact(t *testing.T, target string) Artifact {
	return Artifact{TenantID: "tenant", RuleID: "rule", NodeID: "node", BootID: "boot", RuleRevision: 1, SignerEpoch: 1, PathGeneration: 1, FencingGeneration: 1, BillingSnapshotID: "billing", Intent: Intent{SessionStable: true, Role: "entry", Rule: Model{ID: "rule", Mode: "direct", Protocol: "tcp", Port: freePort(t), Targets: []Target{{Address: target, Weight: 1}}, Scheduling: "weighted_round_robin", ProxyProtocol: "off", IdleTimeoutSeconds: 60, Enabled: true}}}
}
func dialRule(t *testing.T, a Artifact) net.Conn {
	t.Helper()
	c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", a.Intent.Rule.Port), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func roundtrip(t *testing.T, c net.Conn, want string) {
	t.Helper()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, e := c.Write([]byte("x")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, len(want))
	if _, e := io.ReadFull(c, b); e != nil || string(b) != want {
		t.Fatalf("got %q want %q: %v", b, want, e)
	}
}

func TestPrepareCommitHotSessionAndAbortRealSockets(t *testing.T) {
	g := &testGate{}
	m := New(func(Artifact) (accounting.Gate, error) { return g, nil })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := m.Stop(ctx); e != nil {
			t.Error(e)
		}
	})
	a := artifact(t, echoTarget(t, "a"))
	id := [16]byte{1}
	if e := m.Prepare(id, a); e != nil {
		t.Fatal(e)
	}
	before := dialRule(t, a)
	before.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	before.Write([]byte("x"))
	b := make([]byte, 2)
	if n, e := before.Read(b); n != 0 || e == nil {
		t.Fatal("Prepare forwarded before Commit", n, e)
	}
	before.Close()
	if g.opens.Load() != 0 {
		t.Fatal("Prepare admitted accounting session")
	}
	if e := m.Commit(id); e != nil {
		t.Fatal(e)
	}
	old := dialRule(t, a)
	roundtrip(t, old, "ax")
	next := a
	next.RuleRevision = 2
	next.FencingGeneration = 2
	next.Intent.Rule.Targets = []Target{{Address: echoTarget(t, "b"), Weight: 1}}
	if e := m.Prepare([16]byte{2}, next); e != nil {
		t.Fatal(e)
	}
	if e := m.Commit([16]byte{2}); e != nil {
		t.Fatal(e)
	}
	roundtrip(t, old, "ax")
	roundtrip(t, dialRule(t, next), "bx")
	aborted := a
	aborted.RuleID = "other"
	aborted.Intent.Rule.ID = "other"
	aborted.Intent.Rule.Port = freePort(t)
	if e := m.Prepare([16]byte{3}, aborted); e != nil {
		t.Fatal(e)
	}
	if e := m.Abort([16]byte{3}); e != nil {
		t.Fatal(e)
	}
	tx, _ := m.Query([16]byte{3})
	if tx.State != apply.Aborted {
		t.Fatal(tx)
	}
	l, e := net.Listen("tcp", fmt.Sprintf(":%d", aborted.Intent.Rule.Port))
	if e != nil {
		t.Fatal("Abort retained socket", e)
	}
	l.Close()
	if g.bytes.Load() == 0 {
		t.Fatal("real writes bypassed accounting")
	}
}

func TestReleaseProvesClosedSocketsAndReapplyBindsFreshEndpoint(t *testing.T) {
	m := New(func(Artifact) (accounting.Gate, error) { return &testGate{}, nil })
	t.Cleanup(func() { m.Stop(context.Background()) })
	a := artifact(t, echoTarget(t, "a"))
	id := [16]byte{71}
	if err := m.Prepare(id, a); err != nil {
		t.Fatal(err)
	}
	if err := m.Commit(id); err != nil {
		t.Fatal(err)
	}
	c := dialRule(t, a)
	roundtrip(t, c, "ax")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Release(ctx, id); err != nil {
		t.Fatal(err)
	}
	if tx, _ := m.Query(id); tx.State != apply.Released {
		t.Fatal("no released proof", tx)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", a.Intent.Rule.Port), time.Second); err == nil {
		c.Close()
		t.Fatal("released socket remained open")
	}
	a.RuleRevision, a.FencingGeneration = 2, 2
	next := [16]byte{72}
	if err := m.Prepare(next, a); err != nil {
		t.Fatal(err)
	}
	if err := m.Commit(next); err != nil {
		t.Fatal(err)
	}
	roundtrip(t, dialRule(t, a), "ax")
}
