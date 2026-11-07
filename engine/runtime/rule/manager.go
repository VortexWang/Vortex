package rule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"vortex/engine/accounting"
	"vortex/engine/apply"
	corerelay "vortex/engine/relay"
)

type GateFactory func(Artifact) (accounting.Gate, error)
type transaction struct {
	a        Artifact
	state    apply.State
	endpoint *endpoint
	gate     accounting.Gate
}
type endpoint struct {
	tcp         net.Listener
	udp         *net.UDPConn
	mu          sync.Mutex
	current     *transaction
	cancel      context.CancelFunc
	connections map[net.Conn]struct{}
	wg          sync.WaitGroup
}
type Manager struct {
	mu       sync.Mutex
	instance string
	tx       map[[16]byte]*transaction
	active   map[string]*transaction
	gate     GateFactory
	next     uint64
}

func New(gate GateFactory) *Manager {
	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		panic(e)
	}
	return &Manager{instance: hex.EncodeToString(id[:]), tx: map[[16]byte]*transaction{}, active: map[string]*transaction{}, gate: gate}
}
func (m *Manager) InstanceID() string { return m.instance }
func (m *Manager) Query(id [16]byte) (apply.Transaction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tx[id]
	if !ok {
		return apply.Transaction{}, false
	}
	return apply.Transaction{ID: id, Generation: uint64(t.a.FencingGeneration), State: t.state}, true
}

// Prepare owns real TCP/UDP sockets but starts no accept/read goroutine. A
// shared listener is reused on hot edits; established flows retain old gates.
func (m *Manager) Prepare(id [16]byte, a Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.tx[id]; old != nil {
		if old.a.RuleID != a.RuleID || old.a.RuleRevision != a.RuleRevision || old.a.FencingGeneration != a.FencingGeneration {
			return apply.ErrConflict
		}
		return nil
	}
	if m.gate == nil {
		return accounting.ErrDenied
	}
	gate, e := m.gate(a)
	if e != nil {
		return e
	}
	if a.Intent.Rule.Mode != "direct" {
		return errors.New("unsupported_config: VLink rule runtime not prepared")
	}
	old := m.active[a.RuleID]
	if old != nil && a.FencingGeneration <= old.a.FencingGeneration {
		return apply.ErrConflict
	}
	t := &transaction{a: a, state: apply.Prepared, gate: gate}
	model := a.Intent.Rule
	if model.Enabled {
		if old != nil && old.endpoint != nil && old.a.Intent.Rule.Port == model.Port && old.a.Intent.Rule.Protocol == model.Protocol {
			t.endpoint = old.endpoint
		} else {
			ep := &endpoint{connections: map[net.Conn]struct{}{}}
			address := net.JoinHostPort("", strconv.Itoa(int(model.Port)))
			if model.Protocol != "udp" {
				ep.tcp, e = net.Listen("tcp", address)
				if e != nil {
					return e
				}
			}
			if model.Protocol != "tcp" {
				addr, _ := net.ResolveUDPAddr("udp", address)
				ep.udp, e = net.ListenUDP("udp", addr)
				if e != nil {
					if ep.tcp != nil {
						ep.tcp.Close()
					}
					return e
				}
			}
			t.endpoint = ep
		}
	}
	m.tx[id] = t
	return nil
}
func (m *Manager) Commit(id [16]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tx[id]
	if t == nil {
		return apply.ErrConflict
	}
	if t.state == apply.Active {
		return nil
	}
	if t.state != apply.Prepared {
		return apply.ErrConflict
	}
	old := m.active[t.a.RuleID]
	if old != nil && old.a.FencingGeneration >= t.a.FencingGeneration {
		return apply.ErrConflict
	}
	if t.endpoint != nil {
		ep := t.endpoint
		ep.mu.Lock()
		first := ep.current == nil
		ep.current = t
		ep.mu.Unlock()
		if first {
			ctx, cancel := context.WithCancel(context.Background())
			ep.cancel = cancel
			if ep.tcp != nil {
				ep.wg.Add(1)
				go func() { defer ep.wg.Done(); m.serveTCP(ctx, ep) }()
			}
			if ep.udp != nil {
				ep.wg.Add(1)
				go func() { defer ep.wg.Done(); m.serveUDP(ctx, ep) }()
			}
		}
	}
	t.state = apply.Active
	m.active[t.a.RuleID] = t
	if old != nil && old.endpoint != t.endpoint {
		old.state = apply.Released
		old.endpoint.stop(false)
	}
	return nil
}
func (m *Manager) Abort(id [16]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tx[id]
	if t == nil {
		return apply.ErrConflict
	}
	if t.state == apply.Aborted {
		return nil
	}
	if t.state != apply.Prepared {
		return apply.ErrConflict
	}
	if t.endpoint != nil {
		old := m.active[t.a.RuleID]
		if old == nil || old.endpoint != t.endpoint {
			t.endpoint.stop(true)
		}
	}
	t.state = apply.Aborted
	return nil
}
func (e *endpoint) stop(force bool) {
	if e == nil {
		return
	}
	if e.tcp != nil {
		e.tcp.Close()
	}
	if e.udp != nil {
		e.udp.Close()
	}
	if e.cancel != nil {
		e.cancel()
	}
	if force {
		e.mu.Lock()
		for c := range e.connections {
			c.Close()
		}
		e.mu.Unlock()
	}
}
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	eps := map[*endpoint]bool{}
	for _, t := range m.tx {
		if t.endpoint != nil {
			eps[t.endpoint] = true
			t.endpoint.stop(true)
		}
		t.state = apply.Released
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		for ep := range eps {
			ep.wg.Wait()
		}
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
func (m *Manager) serveTCP(ctx context.Context, ep *endpoint) {
	for {
		c, e := ep.tcp.Accept()
		if e != nil {
			return
		}
		ep.mu.Lock()
		t := ep.current
		ep.connections[c] = struct{}{}
		ep.mu.Unlock()
		ep.wg.Add(1)
		go func() {
			defer ep.wg.Done()
			defer c.Close()
			defer func() { ep.mu.Lock(); delete(ep.connections, c); ep.mu.Unlock() }()
			target, e := m.dial(ctx, t, "tcp")
			if e != nil {
				return
			}
			defer target.Close()
			if t.a.Intent.Rule.ProxyProtocol != "off" {
				if e = writeProxy(target, c, t.a.Intent.Rule.ProxyProtocol); e != nil {
					return
				}
			}
			_, _ = corerelay.TCPWithGate(ctx, c, target, t.gate, nil)
		}()
	}
}
func (m *Manager) dial(ctx context.Context, t *transaction, network string) (net.Conn, error) {
	targets := t.a.Intent.Rule.Targets
	m.mu.Lock()
	index := m.next
	m.next++
	m.mu.Unlock()
	total := uint64(0)
	for _, v := range targets {
		total += uint64(v.Weight)
	}
	if total == 0 {
		return nil, accounting.ErrDenied
	}
	selected := int(index % uint64(len(targets)))
	if t.a.Intent.Rule.Scheduling == "weighted_round_robin" {
		n := index % total
		for i, v := range targets {
			if n < uint64(v.Weight) {
				selected = i
				break
			}
			n -= uint64(v.Weight)
		}
	} else {
		for i, v := range targets {
			if v.Priority < targets[selected].Priority {
				selected = i
			}
		}
	}
	var last error
	for step := 0; step < len(targets); step++ {
		v := targets[(selected+step)%len(targets)]
		host, p, e := net.SplitHostPort(v.Address)
		if e != nil {
			return nil, e
		}
		addresses, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			last = e
			continue
		}
		for _, a := range addresses {
			if a.IP.IsLoopback() || a.IP.IsUnspecified() || a.IP.IsMulticast() || a.IP.IsLinkLocalUnicast() {
				return nil, errors.New("rule: target address denied")
			}
		}
		if len(addresses) == 0 {
			continue
		}
		c, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), p))
		if e == nil {
			return c, nil
		}
		last = e
	}
	return nil, last
}
func writeProxy(dst net.Conn, src net.Conn, version string) error {
	a, ok := src.RemoteAddr().(*net.TCPAddr)
	b, x := src.LocalAddr().(*net.TCPAddr)
	if !ok || !x {
		return errors.New("rule: proxy address")
	}
	if version == "v1" {
		family := "TCP6"
		if a.IP.To4() != nil && b.IP.To4() != nil {
			family = "TCP4"
		}
		_, e := fmt.Fprintf(dst, "PROXY %s %s %s %d %d\r\n", family, a.IP, b.IP, a.Port, b.Port)
		return e
	}
	header := []byte("\r\n\r\n\x00\r\nQUIT\n")
	header = append(header, 0x21)
	ipA, ipB := a.IP.To16(), b.IP.To16()
	family := byte(0x21)
	if a.IP.To4() != nil && b.IP.To4() != nil {
		ipA, ipB = a.IP.To4(), b.IP.To4()
		family = 0x11
	}
	n := len(ipA) + len(ipB) + 4
	header = append(header, family, byte(n>>8), byte(n))
	header = append(header, ipA...)
	header = append(header, ipB...)
	header = append(header, byte(a.Port>>8), byte(a.Port), byte(b.Port>>8), byte(b.Port))
	_, e := dst.Write(header)
	return e
}

// UDP keeps the selected target and immutable authorization per client session.
// Session count/queue/idle are bounded; settlement precedes each socket write.
func (m *Manager) serveUDP(ctx context.Context, ep *endpoint) {
	type flow struct {
		conn    net.Conn
		session accounting.Session
		cancel  context.CancelFunc
	}
	flows := map[string]*flow{}
	var mu sync.Mutex
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range flows {
			f.cancel()
			f.conn.Close()
		}
	}()
	buf := make([]byte, 65535)
	for {
		n, peer, e := ep.udp.ReadFromUDP(buf)
		if e != nil {
			return
		}
		key := peer.String()
		mu.Lock()
		f := flows[key]
		mu.Unlock()
		if f == nil {
			mu.Lock()
			full := len(flows) >= 4096
			mu.Unlock()
			if full {
				continue
			}
			ep.mu.Lock()
			t := ep.current
			ep.mu.Unlock()
			session, e := t.gate.Open(ctx, accounting.Flow{Key: key, RuleID: t.a.RuleID})
			if e != nil {
				continue
			}
			conn, e := m.dial(ctx, t, "udp")
			if e != nil {
				session.Close()
				continue
			}
			flowCtx, cancel := context.WithCancel(ctx)
			f = &flow{conn: conn, session: session, cancel: cancel}
			mu.Lock()
			flows[key] = f
			mu.Unlock()
			ep.wg.Add(1)
			go func(f *flow, peer *net.UDPAddr, t *transaction) {
				defer ep.wg.Done()
				defer f.conn.Close()
				defer f.session.Close()
				defer func() { mu.Lock(); delete(flows, peer.String()); mu.Unlock() }()
				out := make([]byte, 65535)
				for {
					f.conn.SetReadDeadline(time.Now().Add(time.Duration(t.a.Intent.Rule.IdleTimeoutSeconds) * time.Second))
					n, e := f.conn.Read(out)
					if e != nil {
						return
					}
					batch, e := f.session.Reserve(flowCtx, accounting.RightToLeft, int64(n))
					if e != nil {
						return
					}
					written, e := ep.udp.WriteToUDP(out[:n], peer)
					if x := batch.Commit(flowCtx, int64(written)); x != nil {
						return
					}
					if e != nil {
						return
					}
				}
			}(f, peer, t)
		}
		batch, e := f.session.Reserve(ctx, accounting.LeftToRight, int64(n))
		if e != nil {
			continue
		}
		written, e := f.conn.Write(buf[:n])
		if x := batch.Commit(ctx, int64(written)); x != nil {
			f.conn.Close()
		}
		if e != nil {
			f.conn.Close()
		}
	}
}
