package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"vortex/engine/runtime/relay"
)

type OperationalEngine interface {
	BeginDrain()
	Drain(context.Context) error
	Stats() relay.Stats
}
type NodeStatus struct {
	BuildSHA256    string `json:"build_sha256,omitempty"`
	DurableStore   bool   `json:"durable_store"`
	NodeID         string `json:"node_id"`
	BootID         string `json:"boot_id"`
	AppliedVersion string `json:"applied_config_version"`
	HubMode        string `json:"hub_mode"`
	ActiveSessions int    `json:"active_sessions"`
	DrainState     string `json:"drain_state"`
}
type adminRequest struct {
	Command   string `json:"command"`
	TimeoutMS int64  `json:"timeout_ms"`
}
type AdminResponse struct {
	Status *NodeStatus `json:"status,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func (n *Node) Status(ctx context.Context) (NodeStatus, error) {
	status := NodeStatus{NodeID: n.NodeID, BootID: n.BootID, AppliedVersion: "0", HubMode: "unknown", DrainState: "running"}
	if engine, ok := n.Engine.(OperationalEngine); ok {
		stats := engine.Stats()
		status.ActiveSessions, status.DrainState = stats.ActiveSessions, stats.DrainState
	}
	if version, err := n.Store.AgentState(ctx, "applied_version"); err == nil {
		status.AppliedVersion = version
	}
	n.statusMu.RLock()
	status.BuildSHA256 = n.buildSHA256
	observer := n.hubModeObserver
	n.statusMu.RUnlock()
	if observer != nil {
		switch mode := observer(); mode {
		case "online", "autonomous", "recovering", "blocked":
			status.HubMode = mode
		}
	}

	info, err := n.Store.Info(ctx)
	if err != nil {
		return status, err
	}
	status.DurableStore = info.JournalMode == "wal" && info.Synchronous == "2"
	return status, nil
}
func (n *Node) Drain(ctx context.Context) error {
	engine, ok := n.Engine.(OperationalEngine)
	if !ok {
		return errors.New("agent: Engine drain unavailable")
	}
	engine.BeginDrain()
	return engine.Drain(ctx)
}

// ServeAdmin binds only a private local Unix socket. The directory and socket
// permissions authenticate access to the owning OS account, not public TCP.
func (n *Node) ServeAdmin(ctx context.Context, path string) (func(), error) {
	if !filepath.IsAbs(path) || len(path) > 100 {
		return nil, errors.New("agent: bounded absolute admin socket path required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("agent: admin directory must be private mode 0700")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("agent: admin directory owner mismatch")
	}
	if existing, err := os.Lstat(path); err == nil {
		if existing.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("agent: admin path is not a socket")
		}
		owner, ok := existing.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) {
			return nil, errors.New("agent: admin socket owner mismatch")
		}
		live, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			live.Close()
			return nil, errors.New("agent: admin socket already active")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, errors.New("agent: cannot establish stale admin socket")
		}
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	var once sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		slots := make(chan struct{}, 8)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				defer conn.Close()
				stop := context.AfterFunc(serveCtx, func() { conn.Close() })
				defer stop()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				raw, readErr := bufio.NewReader(io.LimitReader(conn, 1025)).ReadBytes('\n')
				var request adminRequest
				response := AdminResponse{}
				if readErr != nil || len(raw) > 1024 || decodeAdminRequest(raw, &request) != nil {
					response.Error = "invalid admin request"
				} else if request.Command == "status" {
					status, err := n.Status(serveCtx)
					if err != nil {
						response.Error = "status unavailable"
					} else {
						response.Status = &status
					}
				} else if request.Command == "drain" && request.TimeoutMS > 0 && request.TimeoutMS <= 300000 {
					timeout := time.Duration(request.TimeoutMS) * time.Millisecond
					_ = conn.SetDeadline(time.Now().Add(timeout + 15*time.Second))
					drainCtx, finish := context.WithTimeout(serveCtx, timeout)
					err := n.Drain(drainCtx)
					finish()
					if err != nil {
						response.Error = "drain cleanup incomplete"
					} else {
						status, _ := n.Status(serveCtx)
						response.Status = &status
					}
				} else {
					response.Error = "unsupported admin operation"
				}
				_ = json.NewEncoder(conn).Encode(response)
			}()
		}
	}()
	stop := func() { once.Do(func() { cancel(); listener.Close(); <-done; wg.Wait() }) }
	go func() {
		select {
		case <-serveCtx.Done():
			listener.Close()
		case <-done:
		}
	}()
	return stop, nil
}
func AdminCall(ctx context.Context, path, command string, timeout time.Duration) (NodeStatus, error) {
	if command != "status" && command != "drain" {
		return NodeStatus{}, errors.New("agent: invalid admin command")
	}
	if !filepath.IsAbs(path) || len(path) > 100 {
		return NodeStatus{}, errors.New("agent: invalid admin socket path")
	}
	var socketOwner uint32
	for _, entry := range []struct {
		path   string
		mode   os.FileMode
		socket bool
	}{{filepath.Dir(path), 0700, false}, {path, 0600, true}} {
		info, err := os.Lstat(entry.path)
		if err != nil {
			return NodeStatus{}, err
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (owner.Uid != uint32(os.Geteuid()) && os.Geteuid() != 0) || info.Mode().Perm() != entry.mode || (entry.socket && info.Mode()&os.ModeSocket == 0) || (!entry.socket && !info.IsDir()) {
			return NodeStatus{}, errors.New("agent: insecure admin socket boundary")
		}
		if !entry.socket {
			socketOwner = owner.Uid
		} else if socketOwner != owner.Uid {
			return NodeStatus{}, errors.New("agent: mismatched admin socket owner")
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return NodeStatus{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	info, statErr := os.Lstat(path)
	if statErr != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return NodeStatus{}, errors.New("agent: insecure admin socket")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err = json.NewEncoder(conn).Encode(adminRequest{Command: command, TimeoutMS: timeout.Milliseconds()}); err != nil {
		return NodeStatus{}, err
	}
	var response AdminResponse
	if err = json.NewDecoder(io.LimitReader(conn, 8192)).Decode(&response); err != nil {
		return NodeStatus{}, err
	}
	if response.Error != "" || response.Status == nil {
		return NodeStatus{}, fmt.Errorf("agent: admin operation failed: %s", response.Error)
	}
	return *response.Status, nil
}

func (n *Node) SetHubModeObserver(observer func() string) {
	n.statusMu.Lock()
	n.hubModeObserver = observer
	n.statusMu.Unlock()
}

// DefaultAdminSocket uses a short per-user/per-database private Unix path even
// when the database lives under a long application or test directory. Services
// explicitly override this with their systemd RuntimeDirectory path.
func DefaultAdminSocket(database string) (string, error) {
	absolute, err := filepath.Abs(database)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absolute))
	return fmt.Sprintf("/tmp/vortex-node-%d-%x/admin.sock", os.Geteuid(), sum[:8]), nil
}

// One newline-delimited object, unique known keys, no trailing JSON values.
func decodeAdminRequest(raw []byte, request *adminRequest) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return errors.New("object required")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return errors.New("duplicate key")
		}
		seen[key] = true
		switch key {
		case "command":
			err = dec.Decode(&request.Command)
		case "timeout_ms":
			err = dec.Decode(&request.TimeoutMS)
		default:
			return errors.New("unknown field")
		}
		if err != nil {
			return err
		}
	}
	if _, err = dec.Token(); err != nil {
		return err
	}
	if _, err = dec.Token(); err != io.EOF {
		return errors.New("trailing input")
	}
	return nil
}

func (n *Node) SetBuildSHA256(digest string) {
	n.statusMu.Lock()
	n.buildSHA256 = digest
	n.statusMu.Unlock()
}
