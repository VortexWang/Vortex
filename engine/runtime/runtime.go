package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	engineaccounting "vortex/engine/accounting"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/runtime/relay"
	"vortex/engine/runtime/rule"
	"vortex/engine/vlink"
)

// Runtime owns the Engine data-plane lifecycle used by an Agent process. It
// deliberately accepts an already validated config and exposes lifecycle and drain operations to the control-plane adapter.
type Runtime struct {
	cfg        config.Config
	supervisor *link.Supervisor
	service    *relay.Service
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	err        error
	addr       net.Addr
	mu         sync.Mutex
	started    bool
	rules      *rule.Manager
}

// Done reports the serving goroutine's terminal error.
func (r *Runtime) Done() <-chan struct{} { return r.done }

func (r *Runtime) Addr() net.Addr { r.mu.Lock(); defer r.mu.Unlock(); return r.addr }

// SetAccountingGate permits Agent composition before Start only.
func (r *Runtime) SetAccountingGate(gate engineaccounting.Gate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("runtime: accounting gate must be installed before Start")
	}
	r.service.Gate = gate
	return nil
}

func (r *Runtime) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }

func New(cfg config.Config) (*Runtime, error) { return NewWithAccountingGate(cfg, nil) }

// NewWithAccountingGate injects the Agent-owned in-process authorization and
// durable accounting boundary. Runtime never opens the Agent database itself.
func NewWithAccountingGate(cfg config.Config, gate engineaccounting.Gate) (*Runtime, error) {
	return newRuntime(cfg, gate, false)
}

// NewForAgent requires managed identity composition before Start.
func NewForAgent(cfg config.Config) (*Runtime, error) { return newRuntime(cfg, nil, true) }
func newRuntime(cfg config.Config, gate engineaccounting.Gate, managed bool) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Role != config.RoleDestination || len(cfg.Routes) != 0 {
		return nil, errors.New("runtime: only the static destination runtime is supported")
	}
	cfg = cfg.Effective()
	var supervisor *link.Supervisor
	var err error
	if managed {
		supervisor, err = link.NewManaged(cfg)
	} else {
		supervisor, err = link.New(cfg)
	}
	if err != nil {
		return nil, err
	}
	policy, err := relay.AgentPolicy(cfg)
	if err != nil {
		supervisor.Close()
		return nil, err
	}
	service := &relay.Service{LocalNode: supervisor.NodeID, Policy: policy, Dial: dialTarget, Gate: gate}
	return &Runtime{cfg: cfg, supervisor: supervisor, service: service, done: make(chan struct{})}, nil
}

func (r *Runtime) Start(parent context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("runtime: already started")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if r.supervisor.Signer == nil && !r.supervisor.HasIdentityProvider() {
		return errors.New("runtime: managed identity provider required")
	}
	if !r.cfg.QualificationOnly && r.service.Gate == nil {
		return errors.New("runtime: Agent authorization gate is required")
	}
	listener, err := net.Listen("tcp", r.cfg.Listen)
	if err != nil {
		return fmt.Errorf("runtime: listen %s: %w", r.cfg.Listen, err)
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.addr = listener.Addr()
	r.started = true
	go func() {
		err := r.supervisor.ServeListener(r.ctx, listener, func(ctx context.Context, mux *vlink.MuxLink, peer vlink.NodeID) error {
			if r.rules != nil && r.rules.HasRules() {
				return r.rules.ServeLink(ctx, mux, peer)
			}
			return r.service.ServeLink(ctx, mux)
		})
		r.supervisor.Close()
		r.service.Shutdown()
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
		close(r.done)
	}()
	return nil
}

// SetRuleManager adds artifact routing before Start. Legacy static admission
// remains unchanged until a committed rule is installed.
func (r *Runtime) SetRuleManager(manager *rule.Manager) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || manager == nil {
		return errors.New("runtime: rule manager must be installed before Start")
	}
	r.rules = manager
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	r.cancel()
	r.supervisor.Close()
	r.mu.Unlock()
	select {
	case <-r.done:
		err := r.Err()
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func dialTarget(_ context.Context, network uint8, target vlink.LocalTarget) (net.Conn, error) {
	protocol := "tcp"
	if network == 2 {
		protocol = "udp"
	}
	return net.Dial(protocol, net.JoinHostPort(strings.Trim(target.Host, "[]"), strconv.Itoa(int(target.Port))))
}

// SetIdentityProvider composes Agent-owned credentials without storage access.
func (r *Runtime) SetIdentityProvider(provider link.IdentityProvider, boot vlink.BootID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("runtime: identity provider must be installed before Start")
	}
	return r.supervisor.SetIdentityProvider(provider, boot)
}

// BeginDrain denies new streams without closing established authorized traffic.
func (r *Runtime) BeginDrain()        { r.service.BeginDrain() }
func (r *Runtime) Stats() relay.Stats { return r.service.Stats() }

// Drain stops residual streams on deadline, then joins settlement/worker cleanup.
// A deadline is never financial release; stream cleanup must finish before success.
func (r *Runtime) Drain(ctx context.Context) error {
	if err := r.service.Drain(ctx); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r.supervisor.Close()
		if stopErr := r.service.Drain(cleanup); stopErr != nil {
			return fmt.Errorf("runtime: forced drain cleanup: %w", stopErr)
		}
	}
	return nil
}
