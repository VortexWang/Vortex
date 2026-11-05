//go:build linux

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type processWitness struct {
	KernelBoot, PIDNamespace, StartTick string
	PID                                 int
}

func processTick(pid int) (string, bool, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", false, fmt.Errorf("store: malformed process witness")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return "", false, fmt.Errorf("store: truncated process witness")
	}
	if _, err = strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", false, err
	}
	return fields[19], fields[0] == "Z" || fields[0] == "X", nil
}
func currentProcessWitness() ([]byte, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, err
	}
	ns, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return nil, err
	}
	tick, dead, err := processTick(os.Getpid())
	if err != nil {
		return nil, err
	}
	if dead {
		return nil, fmt.Errorf("store: executor already stopped")
	}
	return json.Marshal(processWitness{strings.TrimSpace(string(boot)), ns, tick, os.Getpid()})
}
func processStopped(raw []byte) (bool, error) {
	var w processWitness
	if err := json.Unmarshal(raw, &w); err != nil {
		return false, err
	}
	if w.PID <= 0 || w.StartTick == "" || w.KernelBoot == "" || w.PIDNamespace == "" {
		return false, fmt.Errorf("store: incomplete execution witness")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return false, err
	}
	ns, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return false, err
	}
	// Namespace/host changes are not an absence proof. This boundary requires
	// observing the same kernel and PID namespace as the original executor.
	if strings.TrimSpace(string(boot)) != w.KernelBoot || ns != w.PIDNamespace {
		return false, nil
	}
	tick, dead, err := processTick(w.PID)
	if err != nil {
		return false, err
	}
	return dead || tick != w.StartTick, nil
}
