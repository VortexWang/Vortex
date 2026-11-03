package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultUDPIdleTimeout = 60 * time.Second

const (
	// DefaultUDPMaxSessions bounds all upstream sessions owned by one relay.
	DefaultUDPMaxSessions = 4096
	// DefaultUDPMaxSessionsPerClient bounds sessions sharing one source IP.
	DefaultUDPMaxSessionsPerClient = 64
)

type UDPOptions struct {
	IdleTimeout time.Duration
	// Zero limits select the finite defaults above. Excess new-client datagrams
	// are dropped before session resources are allocated.
	MaxSessions          int
	MaxSessionsPerClient int
	// OnSessionLimit observes dropped datagrams and runs outside the session lock.
	OnSessionLimit func(client *net.UDPAddr)
}

type udpSession struct {
	client     *net.UDPAddr
	key        string
	conn       *net.UDPConn
	lastActive atomic.Int64
	closeOnce  sync.Once
}

func (s *udpSession) touch(now time.Time) {
	s.lastActive.Store(now.UnixNano())
}

func (s *udpSession) close() {
	s.closeOnce.Do(func() { _ = s.conn.Close() })
}

// UDP forwards datagrams between clients and target, retaining one connected
// upstream socket per client address until it has been idle for IdleTimeout.
// A zero timeout selects DefaultUDPIdleTimeout.
func UDP(ctx context.Context, listener *net.UDPConn, target *net.UDPAddr, options UDPOptions, meter Meter) error {
	if listener == nil || target == nil {
		return errors.New("relay: UDP listener and target are required")
	}
	if options.IdleTimeout < 0 {
		return errors.New("relay: UDP idle timeout cannot be negative")
	}
	if options.MaxSessions < 0 || options.MaxSessionsPerClient < 0 {
		return errors.New("relay: UDP session limits cannot be negative")
	}
	idleTimeout := options.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = DefaultUDPIdleTimeout
	}
	maxSessions := options.MaxSessions
	if maxSessions == 0 {
		maxSessions = DefaultUDPMaxSessions
	}
	maxSessionsPerClient := options.MaxSessionsPerClient
	if maxSessionsPerClient == 0 {
		maxSessionsPerClient = DefaultUDPMaxSessionsPerClient
	}

	sessions := make(map[string]*udpSession)
	clientSessions := make(map[string]int)
	var sessionsMu sync.Mutex
	var readers sync.WaitGroup
	closeSessions := func() {
		sessionsMu.Lock()
		for key, session := range sessions {
			delete(sessions, key)
			session.close()
		}
		clear(clientSessions)
		sessionsMu.Unlock()
	}

	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = listener.Close()
			closeSessions()
		case <-stopWatch:
		}
	}()
	defer func() {
		close(stopWatch)
		<-watchDone
		_ = listener.Close()
		closeSessions()
		readers.Wait()
	}()

	errCh := make(chan error, 1)
	fail := func(err error) {
		select {
		case errCh <- err:
		default:
		}
		_ = listener.Close()
	}

	removeSession := func(session *udpSession) {
		sessionsMu.Lock()
		deleteSessionLocked(sessions, clientSessions, session)
		sessionsMu.Unlock()
		session.close()
	}

	readBuffer := make([]byte, 64*1024)

	newSession := func(client *net.UDPAddr, key string) (*udpSession, bool, error) {
		clientKey := client.IP.String()
		if len(sessions) >= maxSessions || clientSessions[clientKey] >= maxSessionsPerClient {
			return nil, true, nil
		}
		upstream, err := net.DialUDP("udp", nil, target)
		if err != nil {
			return nil, false, fmt.Errorf("relay: dial UDP target: %w", err)
		}
		session := &udpSession{client: cloneUDPAddr(client), key: key, conn: upstream}
		session.touch(time.Now())
		sessions[key] = session
		clientSessions[clientKey]++
		readers.Add(1)
		go func() {
			defer readers.Done()
			responseBuffer := make([]byte, 64*1024)
			for {
				n, err := session.conn.Read(responseBuffer)
				if err != nil {
					removeSession(session)
					return
				}
				session.touch(time.Now())
				sessionsMu.Lock()
				if sessions[session.key] != session {
					sessionsMu.Unlock()
					continue
				}
				written, writeErr := listener.WriteToUDP(responseBuffer[:n], session.client)
				sessionsMu.Unlock()
				if written > 0 && meter != nil {
					if meter(RightToLeft, int64(written)) != nil {
						fail(errors.New("relay: UDP response metering failed"))
						return
					}
				}
				if writeErr != nil {
					if ctx.Err() == nil {
						fail(fmt.Errorf("relay: write UDP response: %w", writeErr))
					}
					return
				}
				if written != n {
					fail(ErrShortWrite)
					return
				}
			}
		}()
		return session, false, nil
	}

	janitorEvery := idleTimeout / 4
	if janitorEvery > time.Second || janitorEvery <= 0 {
		janitorEvery = time.Second
	}
	if janitorEvery < 10*time.Millisecond {
		janitorEvery = 10 * time.Millisecond
	}
	janitorStop := make(chan struct{})
	janitorDone := make(chan struct{})
	go func() {
		defer close(janitorDone)
		ticker := time.NewTicker(janitorEvery)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				sessionsMu.Lock()
				for key, session := range sessions {
					lastActive := time.Unix(0, session.lastActive.Load())
					if now.Sub(lastActive) >= idleTimeout {
						delete(sessions, key)
						clientSessions[session.client.IP.String()]--
						if clientSessions[session.client.IP.String()] == 0 {
							delete(clientSessions, session.client.IP.String())
						}
						session.close()
					}
				}
				sessionsMu.Unlock()
			case <-janitorStop:
				return
			}
		}
	}()
	defer func() {
		close(janitorStop)
		<-janitorDone
	}()

	for {
		n, client, err := listener.ReadFromUDP(readBuffer)
		if err != nil {
			select {
			case fatal := <-errCh:
				return fatal
			default:
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("relay: read UDP client datagram: %w", err)
		}
		key := client.String()
		sessionsMu.Lock()
		session := sessions[key]
		if session != nil && time.Since(time.Unix(0, session.lastActive.Load())) >= idleTimeout {
			deleteSessionLocked(sessions, clientSessions, session)
			session.close()
			session = nil
		}
		limited := false
		if session == nil {
			session, limited, err = newSession(client, key)
		}
		if session != nil {
			session.touch(time.Now())
		}
		sessionsMu.Unlock()
		if limited {
			if options.OnSessionLimit != nil {
				options.OnSessionLimit(cloneUDPAddr(client))
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("relay: create UDP session: %w", err)
		}
		written, err := session.conn.Write(readBuffer[:n])
		if written > 0 && meter != nil {
			if meter(LeftToRight, int64(written)) != nil {
				return errors.New("relay: UDP request metering failed")
			}
		}
		if err != nil {
			removeSession(session)
			return fmt.Errorf("relay: write UDP target datagram: %w", err)
		}
		if written != n {
			removeSession(session)
			return ErrShortWrite
		}
	}
}

func deleteSessionLocked(sessions map[string]*udpSession, clientSessions map[string]int, session *udpSession) bool {
	if sessions[session.key] != session {
		return false
	}
	delete(sessions, session.key)
	clientKey := session.client.IP.String()
	clientSessions[clientKey]--
	if clientSessions[clientKey] == 0 {
		delete(clientSessions, clientKey)
	}
	return true
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	clone := *addr
	clone.IP = append(net.IP(nil), addr.IP...)
	return &clone
}
