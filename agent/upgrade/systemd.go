package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SystemdService operates only a validated fixed service name and release link.
// Commands use argv, never shell interpolation. The upgrader is a separate
// process with service-manager privileges, not an Agent database writer.
type SystemdService struct {
	Unit       string
	ActiveLink string
}

func (s *SystemdService) validate() error {
	if !strings.HasSuffix(s.Unit, ".service") || len(s.Unit) > 128 || len(s.Unit) < 9 || s.Unit[0] == '-' {
		return errors.New("upgrade: invalid service name")
	}
	for _, c := range s.Unit {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.@-", c)) {
			return errors.New("upgrade: invalid service name")
		}
	}
	if !filepath.IsAbs(s.ActiveLink) {
		return errors.New("upgrade: absolute release link required")
	}
	return nil
}
func (s *SystemdService) command(ctx context.Context, args ...string) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("upgrade: service manager command failed: %w", err)
	}
	if len(out) > 32768 {
		return nil, errors.New("upgrade: oversized service manager response")
	}
	return out, nil
}
func (s *SystemdService) inspect(ctx context.Context) (map[string]string, error) {
	raw, err := s.command(ctx, "show", "--no-pager", "--property=LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,KillMode", s.Unit)
	if err != nil {
		return nil, err
	}
	state := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, errors.New("upgrade: malformed service state")
		}
		state[key] = value
	}
	if state["LoadState"] != "loaded" || (state["KillMode"] != "control-group" && state["KillMode"] != "mixed") {
		return nil, errors.New("upgrade: loaded whole-service kill policy required")
	}
	return state, nil
}
func cgroupUnpopulated(group string) (bool, error) {
	if group == "" {
		return true, nil
	}
	if !strings.HasPrefix(group, "/") || filepath.Clean(group) != group || group == "/" {
		return false, errors.New("upgrade: invalid service cgroup")
	}
	raw, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", group, "cgroup.events"))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "populated 0" {
			return true, nil
		}
		if line == "populated 1" {
			return false, nil
		}
	}
	return false, errors.New("upgrade: cgroup population unavailable")
}
func (s *SystemdService) StopAndWait(ctx context.Context) error {
	before, err := s.inspect(ctx)
	if err != nil {
		return err
	}
	// A failed stop command never becomes a successful witness just because its
	// caller timed out. The durable installer keeps unknown until a later query.
	if _, err = s.command(ctx, "stop", s.Unit); err != nil {
		return err
	}
	for {
		state, err := s.inspect(ctx)
		if err != nil {
			return err
		}
		stopped := (state["ActiveState"] == "inactive" || state["ActiveState"] == "failed") && state["MainPID"] == "0" && state["ControlPID"] == "0"
		if stopped {
			old, err := cgroupUnpopulated(before["ControlGroup"])
			if err != nil {
				return err
			}
			current, err := cgroupUnpopulated(state["ControlGroup"])
			if err != nil {
				return err
			}
			if old && current {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func fileDigest(path string) (string, error) {
	// Release artifacts are bounded by the command's preflight. Avoid reading
	// arbitrary devices/directories during an executable identity check.
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return "", errors.New("upgrade: invalid executable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *SystemdService) Start(ctx context.Context, release string) error {
	if err := s.validate(); err != nil {
		return err
	}
	selected, err := filepath.EvalSymlinks(s.ActiveLink)
	if err != nil {
		return err
	}
	expected, err := filepath.EvalSymlinks(release)
	if err != nil {
		return err
	}
	if selected != expected {
		return errors.New("upgrade: service release selection changed")
	}
	digest, err := fileDigest(expected)
	if err != nil {
		return err
	}
	if _, err = s.command(ctx, "start", s.Unit); err != nil {
		return err
	}
	for {
		state, err := s.inspect(ctx)
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(state["MainPID"])
		if err != nil || pid < 0 {
			return errors.New("upgrade: invalid service pid")
		}
		if state["ActiveState"] == "failed" || state["ActiveState"] == "inactive" {
			return errors.New("upgrade: service exited during activation")
		}
		if state["ActiveState"] == "active" && pid > 0 {
			actual, err := fileDigest(fmt.Sprintf("/proc/%d/exe", pid))
			if err != nil {
				return err
			}
			if actual != digest {
				return errors.New("upgrade: running executable differs from selected release")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
