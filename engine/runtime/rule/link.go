package rule

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
	"vortex/engine/accounting"
	"vortex/engine/apply"
	corerelay "vortex/engine/relay"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

// SetLinkFactory supplies Agent-selected trust/identity through an in-process
// boundary. The factory cannot change after any transaction is prepared.
func (m *Manager) SetLinkFactory(factory func(Artifact, Member) (*link.Supervisor, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tx) != 0 || factory == nil {
		return apply.ErrConflict
	}
	m.linkFactory = factory
	return nil
}
func (m *Manager) HasRules() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.active) > 0 }

// ServeLink accepts only TLS/HELLO authenticated muxes from the node listener.
// Registered reverse links are reusable, but each OPEN independently checks the
// committed rule, upstream boot, hop, visited set and generation.
func (m *Manager) ServeLink(ctx context.Context, mux *vlink.MuxLink, peer vlink.NodeID) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return context.Canceled
	}
	key := peer.String()
	if m.links[key] == nil {
		m.links[key] = map[*vlink.MuxLink]struct{}{}
	}
	m.links[key][mux] = struct{}{}
	m.workers.Add(1)
	m.mu.Unlock()
	defer m.workers.Done()
	defer func() { m.mu.Lock(); delete(m.links[key], mux); m.mu.Unlock() }()
	for {
		stream, e := mux.Accept(ctx)
		if e != nil {
			return e
		}
		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return context.Canceled
		}
		m.workers.Add(1)
		m.mu.Unlock()
		go func() { defer m.workers.Done(); m.serveStream(ctx, mux, stream, peer) }()
	}
}

type streamTarget struct {
	conn    net.Conn
	session accounting.Session
}

func (t *streamTarget) Close() error { defer t.session.Close(); return t.conn.Close() }
func (m *Manager) serveStream(ctx context.Context, mux *vlink.MuxLink, stream *vlink.MuxStream, peer vlink.NodeID) {
	e := mux.AuthorizeRemote(ctx, stream.ID, func(ctx context.Context, authenticated vlink.NodeID, r vlink.OpenRequest) (io.Closer, error) {
		m.mu.Lock()
		t := m.active[vlink.NodeID(r.RuleID).String()]
		m.mu.Unlock()
		if t == nil || !t.a.Intent.Rule.Enabled || t.a.Intent.Hop == 0 || authenticated != peer {
			return nil, accounting.ErrDenied
		}
		a := t.a
		local, e := vlink.ParseNodeID(a.NodeID)
		if e != nil {
			return nil, e
		}
		boot, _ := mux.PeerIncarnation()
		allowed := false
		for _, member := range a.Intent.UpstreamMembers {
			if member.ID == peer.String() && member.BootID == vlink.NodeID(boot).String() {
				allowed = true
			}
		}
		if !allowed {
			return nil, accounting.ErrDenied
		}
		networks := map[uint8]bool{1: a.Intent.Rule.Protocol != "udp", 2: a.Intent.Rule.Protocol != "tcp"}
		snapshot := vlink.RuleSnapshot{RuleID: r.RuleID, Revision: uint64(a.RuleRevision), PathGeneration: uint64(a.PathGeneration), HopIndex: uint16(a.Intent.Hop), Active: true, AllowedPeers: map[vlink.NodeID]bool{peer: true}, AllowedNetworks: networks, RuntimeAuthorized: true, SegmentAllowed: true, NextHops: []vlink.NodeID{local}, ExecutionPoint: a.Intent.Role == "exit", SelfHosted: true}
		policy := vlink.OpenPolicy{LocalNode: local, Lookup: func(context.Context, [16]byte) (vlink.RuleSnapshot, bool) { return snapshot, true }}
		if _, e = policy.Authorize(ctx, peer, r); e != nil {
			return nil, e
		}
		session, e := t.gate.Open(ctx, accounting.Flow{Key: stream.FlowKey(), RuleID: a.RuleID})
		if e != nil {
			return nil, e
		}
		network := "tcp"
		if r.Network == 2 {
			network = "udp"
		}
		var target net.Conn
		if a.Intent.Role == "exit" {
			target, e = m.dial(ctx, t, network)
		} else {
			r.HopIndex++
			r.Visited = append(append([]vlink.NodeID(nil), r.Visited...), local)
			target, e = m.openNext(ctx, t, r)
		}
		if e != nil {
			session.Close()
			return nil, e
		}
		// The OPEN client IP is preserved across every hop. PROXY emission is only
		// at the terminal target, never inserted into inter-node business framing.
		if a.Intent.Role == "exit" && a.Intent.Rule.ProxyProtocol != "off" {
			remote := &addressConn{Conn: target, remote: &net.TCPAddr{IP: net.IP(r.ClientAddress)}}
			if e = writeProxy(target, remote, a.Intent.Rule.ProxyProtocol); e != nil {
				target.Close()
				session.Close()
				return nil, e
			}
		}
		return &streamTarget{target, session}, nil
	})
	if e != nil {
		return
	}
	resource, ok := stream.TakeResource().(*streamTarget)
	if !ok {
		return
	}
	defer resource.Close()
	left := newMuxConn(ctx, stream, func() {})
	if stream.Network == vlink.StreamTCP {
		_, _ = corerelay.TCPWithGate(ctx, left, resource.conn, oneSessionGate{resource.session}, nil)
		return
	}
	bridgeDatagrams(ctx, left, resource.conn, resource.session)
}

type addressConn struct {
	net.Conn
	remote net.Addr
}

func (c *addressConn) RemoteAddr() net.Addr { return c.remote }

type oneSessionGate struct{ session accounting.Session }

func (g oneSessionGate) Open(context.Context, accounting.Flow) (accounting.Session, error) {
	return g.session, nil
}

func (m *Manager) connect(ctx context.Context, t *transaction, network string, original net.Addr) (net.Conn, error) {
	if t.a.Intent.Rule.Mode == "direct" {
		return m.dial(ctx, t, network)
	}
	rule, e := vlink.ParseNodeID(t.a.RuleID)
	if e != nil {
		return nil, e
	}
	local, e := vlink.ParseNodeID(t.a.NodeID)
	if e != nil {
		return nil, e
	}
	host, _, e := net.SplitHostPort(original.String())
	if e != nil {
		return nil, e
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, accounting.ErrDenied
	}
	if v := ip.To4(); v != nil {
		ip = v
	}
	netID := uint8(1)
	if network == "udp" {
		netID = 2
	}
	return m.openNext(ctx, t, vlink.OpenRequest{RuleID: [16]byte(rule), RuleRevision: uint64(t.a.RuleRevision), PathGeneration: uint64(t.a.PathGeneration), HopIndex: uint16(t.a.Intent.Hop + 1), Network: netID, Visited: []vlink.NodeID{local}, ClientAddress: []byte(ip)})
}
func (m *Manager) openNext(ctx context.Context, t *transaction, r vlink.OpenRequest) (net.Conn, error) {
	a := t.a
	if len(a.Intent.NextMembers) == 0 || a.Intent.Hop >= len(a.Intent.Segments) {
		return nil, accounting.ErrDenied
	}
	members := a.Intent.NextMembers
	m.mu.Lock()
	index := m.next
	m.next++
	m.mu.Unlock()
	total := uint64(0)
	for _, member := range members {
		total += uint64(member.Weight)
	}
	if total == 0 {
		return nil, accounting.ErrDenied
	}
	selected := 0
	n := index % total
	for i, member := range members {
		if n < uint64(member.Weight) {
			selected = i
			break
		}
		n -= uint64(member.Weight)
	}
	if a.Intent.NextScheduling == "primary_backup" {
		for i, member := range members {
			if member.Priority < members[selected].Priority {
				selected = i
			}
		}
	}
	var last error
	for i := range members {
		member := members[(selected+i)%len(members)]
		var mux *vlink.MuxLink
		release := func() {}
		if a.Intent.Segments[a.Intent.Hop].Direction == "reverse" {
			m.mu.Lock()
			for candidate := range m.links[member.ID] {
				boot, _ := candidate.PeerIncarnation()
				select {
				case <-candidate.Closed():
					continue
				default:
				}
				if vlink.NodeID(boot).String() == member.BootID {
					mux = candidate
					break
				}
			}
			m.mu.Unlock()
			if mux == nil {
				last = errors.New("rule: reverse peer not connected")
				continue
			}
		} else {
			var e error
			mux, release, e = m.dialLink(ctx, a, member)
			if e != nil {
				last = e
				continue
			}
		}
		stream, e := mux.Open(ctx, r)
		if e == nil {
			e = stream.WaitOpen(ctx)
		}
		if e != nil {
			release()
			last = e
			continue
		}
		return newMuxConn(ctx, stream, release), nil
	}
	if last == nil {
		last = accounting.ErrDenied
	}
	return nil, last
}

func (m *Manager) dialLink(ctx context.Context, a Artifact, member Member) (*vlink.MuxLink, func(), error) {
	m.mu.Lock()
	factory := m.linkFactory
	if m.stopped {
		m.mu.Unlock()
		return nil, nil, context.Canceled
	}
	m.workers.Add(1)
	m.mu.Unlock()
	if factory == nil {
		m.workers.Done()
		return nil, nil, accounting.ErrDenied
	}
	supervisor, e := factory(a, member)
	if e != nil {
		m.workers.Done()
		return nil, nil, e
	}
	linkCtx, cancel := context.WithCancel(ctx)
	ready := make(chan *vlink.MuxLink, 1)
	done := make(chan error, 1)
	go func() {
		defer m.workers.Done()
		defer supervisor.Close()
		done <- supervisor.Dial(linkCtx, func(ctx context.Context, mux *vlink.MuxLink, peer vlink.NodeID) error {
			boot, _ := mux.PeerIncarnation()
			if vlink.NodeID(boot).String() != member.BootID {
				return accounting.ErrDenied
			}
			ready <- mux
			return m.ServeLink(ctx, mux, peer)
		})
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case mux := <-ready:
		return mux, cancel, nil
	case e := <-done:
		cancel()
		return nil, nil, e
	case <-timer.C:
		cancel()
		return nil, nil, context.DeadlineExceeded
	case <-ctx.Done():
		cancel()
		return nil, nil, ctx.Err()
	}
}
func (m *Manager) maintainReverse(ctx context.Context, a Artifact, peer Member) {
	for {
		mux, release, e := m.dialLink(ctx, a, peer)
		if e == nil {
			select {
			case <-ctx.Done():
				release()
				return
			case <-mux.Closed():
				release()
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func bridgeDatagrams(ctx context.Context, left, right net.Conn, session accounting.Session) {
	done := make(chan struct{})
	var once sync.Once
	closeBoth := func() { once.Do(func() { left.Close(); right.Close() }) }
	defer closeBoth()
	go func() {
		select {
		case <-ctx.Done():
			closeBoth()
		case <-done:
		}
	}()
	errors := make(chan error, 2)
	forward := func(src, dst net.Conn, direction accounting.Direction) {
		buffer := make([]byte, 65535)
		for {
			n, e := src.Read(buffer)
			if e != nil {
				errors <- e
				return
			}
			batch, e := session.Reserve(ctx, direction, int64(n))
			if e != nil {
				errors <- e
				return
			}
			written, e := dst.Write(buffer[:n])
			if e != nil {
				batch.MarkUnknown(context.WithoutCancel(ctx))
				errors <- e
				return
			}
			if e = batch.Commit(ctx, int64(written)); e != nil {
				errors <- e
				return
			}
		}
	}
	go forward(left, right, accounting.LeftToRight)
	go forward(right, left, accounting.RightToLeft)
	<-errors
	closeBoth()
	<-errors
	close(done)
}
