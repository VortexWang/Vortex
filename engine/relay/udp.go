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

type UDPOptions struct {
	IdleTimeout time.Duration
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
	idleTimeout := options.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = DefaultUDPIdleTimeout
	}

	sessions := make(map[string]*udpSession)
	var sessionsMu sync.Mutex
	var readers sync.WaitGroup
	closeSessions := func() {
		sessionsMu.Lock()
		for key, session := range sessions {
			delete(sessions, key)
			session.close()
		}
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
		if sessions[session.key] == session {
			delete(sessions, session.key)
		}
		sessionsMu.Unlock()
		session.close()
	}

	readBuffer := make([]byte, 64*1024)

	newSession := func(client *net.UDPAddr, key string) (*udpSession, error) {
		upstream, err := net.DialUDP("udp", nil, target)
		if err != nil {
			return nil, fmt.Errorf("relay: dial UDP target: %w", err)
		}
		session := &udpSession{client: cloneUDPAddr(client), key: key, conn: upstream}
		session.touch(time.Now())
		sessions[key] = session
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
		return session, nil
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
			delete(sessions, key)
			session.close()
			session = nil
		}
		if session == nil {
			session, err = newSession(client, key)
		}
		if session != nil {
			session.touch(time.Now())
		}
		sessionsMu.Unlock()
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

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	clone := *addr
	clone.IP = append(net.IP(nil), addr.IP...)
	return &clone
}
