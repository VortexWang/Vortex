package runtime

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/runtime/relay"
	"vortex/engine/vlink"
)

// Runtime owns the Engine data-plane lifecycle used by an Agent process. It
// deliberately accepts an already validated config and exposes only Start and
// Stop to the control-plane adapter.
type Runtime struct {
	cfg        config.Config
	supervisor *link.Supervisor
	service    *relay.Service
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan error
	mu         sync.Mutex
	started    bool
}

// Done reports the serving goroutine's terminal error.
func (r *Runtime) Done() <-chan error { return r.done }

func New(cfg config.Config) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.Effective()
	supervisor, err := link.New(cfg)
	if err != nil {
		return nil, err
	}
	policy, err := relay.StaticPolicy(cfg)
	if err != nil {
		supervisor.Close()
		return nil, err
	}
	service := &relay.Service{LocalNode: supervisor.NodeID, Policy: policy, Dial: dialTarget}
	return &Runtime{cfg: cfg, supervisor: supervisor, service: service, done: make(chan error, 1)}, nil
}

func (r *Runtime) Start(parent context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("runtime: already started")
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.started = true
	go func() {
		err := r.supervisor.Serve(r.ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
			return r.service.ServeLink(ctx, mux)
		})
		r.done <- err
	}()
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
	r.service.Shutdown()
	select {
	case err := <-r.done:
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
