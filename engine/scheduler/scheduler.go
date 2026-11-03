package scheduler

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrClosed    = errors.New("scheduler: closed")
	ErrQueueFull = errors.New("scheduler: queue full")
)

type Kind uint8

const (
	TCP Kind = 1
	UDP Kind = 2
)

type DropReason string

const (
	DropQueueFull  DropReason = "queue_full"
	DropOversize   DropReason = "oversize"
	DropDownstream DropReason = "downstream"
)

type Item struct {
	Kind     Kind
	StreamID uint32
	Payload  []byte
}
type Scheduler struct {
	mu                   sync.Mutex
	tcp, udp             []Item
	udpBytes, udpLimit   uint64
	roundBytes, roundUDP uint64
	closed               bool
	notify               chan struct{}
	dropped              map[DropReason]uint64
}

func New(udpLimit uint64) *Scheduler {
	if udpLimit == 0 {
		udpLimit = 1 << 20
	}
	return &Scheduler{udpLimit: udpLimit, notify: make(chan struct{}), dropped: make(map[DropReason]uint64)}
}
func (s *Scheduler) Enqueue(item Item) (bool, DropReason) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, DropDownstream
	}
	if len(item.Payload) == 0 {
		return false, DropOversize
	}
	if item.Kind == UDP {
		if s.udpBytes+uint64(len(item.Payload)) > s.udpLimit {
			s.dropped[DropQueueFull]++
			return false, DropQueueFull
		}
		s.udp = append(s.udp, clone(item))
		s.udpBytes += uint64(len(item.Payload))
	} else {
		s.tcp = append(s.tcp, clone(item))
	}
	s.broadcastLocked()
	return true, ""
}
func (s *Scheduler) Next(ctx context.Context) (Item, error) {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return Item{}, ErrClosed
		}
		if len(s.tcp) > 0 || len(s.udp) > 0 {
			item := s.nextLocked()
			s.mu.Unlock()
			return item, nil
		}
		n := s.notify
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Item{}, ctx.Err()
		case <-n:
		}
	}
}
func (s *Scheduler) nextLocked() Item {
	if len(s.udp) > 0 && (len(s.tcp) == 0 || s.roundUDP*2 < s.roundBytes+uint64(len(s.udp[0].Payload))) {
		item := s.udp[0]
		s.udp = s.udp[1:]
		s.udpBytes -= uint64(len(item.Payload))
		s.roundUDP += uint64(len(item.Payload))
		s.roundBytes += uint64(len(item.Payload))
		if s.roundBytes >= 64*1024 {
			s.roundBytes, s.roundUDP = 0, 0
		}
		return item
	}
	item := s.tcp[0]
	s.tcp = s.tcp[1:]
	s.roundBytes += uint64(len(item.Payload))
	if s.roundBytes >= 64*1024 {
		s.roundBytes, s.roundUDP = 0, 0
	}
	return item
}
func (s *Scheduler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.broadcastLocked()
	}
}
func (s *Scheduler) Drops(reason DropReason) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped[reason]
}
func (s *Scheduler) broadcastLocked() { close(s.notify); s.notify = make(chan struct{}) }
func clone(i Item) Item               { i.Payload = append([]byte(nil), i.Payload...); return i }
