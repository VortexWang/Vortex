package vlink

import (
	"errors"
	"sync"
	"time"
)

type LinkAction uint8

const (
	LinkNoAction LinkAction = iota
	LinkSendPing
	LinkPingExpired
	LinkUnavailable
)

type Liveness struct {
	mu                     sync.Mutex
	lastReceived, lastPing time.Time
	pingOutstanding        bool
	pingSent               time.Time
}

func NewLiveness(now time.Time) *Liveness { return &Liveness{lastReceived: now} }

func (l *Liveness) Received(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.After(l.lastReceived) {
		l.lastReceived = now
	}
	l.pingOutstanding = false
}

func (l *Liveness) Poll(now time.Time) LinkAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.lastReceived) {
		return LinkNoAction
	}
	if now.Sub(l.lastReceived) >= 12*time.Second {
		return LinkUnavailable
	}
	if l.pingOutstanding {
		if now.Sub(l.pingSent) >= 3*time.Second {
			l.pingOutstanding = false
			l.lastPing = now
			return LinkPingExpired
		}
		return LinkNoAction
	}
	if now.Sub(l.lastReceived) >= 3*time.Second && (l.lastPing.IsZero() || now.Sub(l.lastPing) >= 3*time.Second) {
		l.pingOutstanding = true
		l.pingSent = now
		return LinkSendPing
	}
	return LinkNoAction
}

type Dispatcher struct {
	mu         sync.Mutex
	streams    *StreamTable
	liveness   *Liveness
	now        func() time.Time
	closed     chan struct{}
	closeErr   error
	closedOnce sync.Once
}

func NewDispatcher(localClient bool, maxPendingOpenBytes int) *Dispatcher {
	return NewDispatcherWithClock(localClient, maxPendingOpenBytes, time.Now)
}

func NewDispatcherWithClock(localClient bool, maxPendingOpenBytes int, now func() time.Time) *Dispatcher {
	if now == nil {
		now = time.Now
	}
	return &Dispatcher{streams: NewStreamTable(localClient, maxPendingOpenBytes), liveness: NewLiveness(now()), now: now, closed: make(chan struct{})}
}

func (d *Dispatcher) Open(network StreamNetwork) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closeErr != nil {
		return 0, d.closeErr
	}
	return d.streams.Open(network)
}

func (d *Dispatcher) Apply(frame Frame) (Frame, error) {
	d.mu.Lock()
	closed := d.closeErr
	d.mu.Unlock()
	if closed != nil {
		return Frame{}, closed
	}
	accepted, err := d.streams.Apply(frame)
	if err != nil && errors.Is(err, ErrProtocol) {
		d.Close(err)
	} else if err == nil {
		d.liveness.Received(d.now())
	}
	return accepted, err
}

func (d *Dispatcher) PollLiveness() LinkAction { return d.liveness.Poll(d.now()) }

func (d *Dispatcher) IdentityFailure(err error) {
	if err == nil {
		err = ErrProtocol
	}
	d.Close(err)
}

func (d *Dispatcher) Close(err error) {
	if err == nil {
		err = ErrProtocol
	}
	d.closedOnce.Do(func() { d.mu.Lock(); d.closeErr = err; d.streams.CloseAll(); close(d.closed); d.mu.Unlock() })
}

func (d *Dispatcher) Closed() <-chan struct{} { return d.closed }
func (d *Dispatcher) Err() error              { d.mu.Lock(); defer d.mu.Unlock(); return d.closeErr }
func (d *Dispatcher) StreamCount() int        { return d.streams.Count() }
