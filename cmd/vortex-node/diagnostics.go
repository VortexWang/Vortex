package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
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
	runtime.SetBlockProfileRate(1)
	previous := runtime.SetMutexProfileFraction(1)
	return func() {
		pprof.StopCPUProfile()
		_ = cpu.Close()
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
