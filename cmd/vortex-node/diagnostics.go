package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

// Local opt-in only; no network profiler endpoint. A fresh directory prevents
// overwriting an earlier run. Profiles describe instrumented execution only.
func startDiagnostics(dir string) (func(), error) {
	if dir == "" {
		return func() {}, nil
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, fmt.Errorf("create diagnostics directory: %w", err)
	}
	cpu, err := os.OpenFile(filepath.Join(dir, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		return nil, err
	}
	// This isolated diagnostic branch samples CPU before the original process
	// deadline can kill the child. It never changes forwarding or test budgets.
	cpuOnly := os.Getenv("VORTEX_DIAGNOSTIC_CPU_ONLY") == "1"
	rate := 1
	if cpuOnly {
		rate = 0
	}
	runtime.SetBlockProfileRate(rate)
	previous := runtime.SetMutexProfileFraction(rate)
	var once sync.Once
	stopCPU := func() { once.Do(func() { pprof.StopCPUProfile(); _ = cpu.Close() }) }
	var timer *time.Timer
	if cpuOnly {
		timer = time.AfterFunc(20*time.Second, stopCPU)
	}
	return func() {
		if timer != nil {
			timer.Stop()
		}
		stopCPU()
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(previous)
		for _, name := range []string{"block", "mutex"} {
			f, err := os.OpenFile(filepath.Join(dir, name+".pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			if err := pprof.Lookup(name).WriteTo(f, 0); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			_ = f.Close()
		}
	}, nil
}
