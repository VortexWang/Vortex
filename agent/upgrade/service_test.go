package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type testService struct {
	mu      sync.Mutex
	events  []string
	stopErr error
	running bool
}

func (s *testService) StopAndWait(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "stop")
	if s.stopErr != nil {
		return s.stopErr
	}
	s.running = false
	return ctx.Err()
}
func (s *testService) Start(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("overlapping service")
	}
	s.running = true
	s.events = append(s.events, "start:"+filepath.Base(filepath.Dir(path)))
	return ctx.Err()
}

func TestServiceStopUnknownCannotActivateAndRecoveryUsesSameTransaction(t *testing.T) {
	f := newInstallerFixture(t)
	service := &testService{running: true, stopErr: errors.New("lost stop reply")}
	f.installer.Service = service
	request := f.request("20112233-4455-6677-8899-aabbccddeeff")
	if err := f.installer.Install(context.Background(), request); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal(err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "process-stop-unknown" {
		t.Fatalf("%+v %v", state, err)
	}
	target, _ := os.Readlink(f.active)
	if target != "releases/old/vortex-node" {
		t.Fatal("unproven stop switched binary")
	}
	if err = f.installer.Recover(context.Background()); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal("recovery invented stop proof", err)
	}
	service.stopErr = nil
	if err = f.installer.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ = f.installer.readState()
	if state.TransactionID != request.TransactionID || state.State != "not-activated" {
		t.Fatalf("%+v", state)
	}
	if err = f.installer.Install(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRollbackStopsCandidateBeforeRestoringBinary(t *testing.T) {
	f := newInstallerFixture(t)
	service := &testService{running: true}
	f.installer.Service = service
	f.installer.Health = func(_ context.Context, path string) error {
		service.events = append(service.events, "health:"+filepath.Base(filepath.Dir(path)))
		if !strings.Contains(path, "/old/") {
			return errors.New("candidate cannot forward")
		}
		return nil
	}
	f.installer.RestoreCompatible = func(context.Context, []byte) error {
		if service.running {
			t.Fatal("restore while candidate still alive")
		}
		service.events = append(service.events, "restore-nonfinancial")
		return nil
	}
	if err := f.installer.Install(context.Background(), f.request("30112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("unhealthy candidate passed")
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "rolled-back" || !service.running {
		t.Fatalf("%+v %v", state, err)
	}
	expected := "stop,start:30112233-4455-6677-8899-aabbccddeeff,health:30112233-4455-6677-8899-aabbccddeeff,stop,restore-nonfinancial,start:old,health:old"
	if got := strings.Join(service.events, ","); got != expected {
		t.Fatalf("order %s", got)
	}
}

func TestServiceRollbackStopFailureRetainsDurableIntent(t *testing.T) {
	f := newInstallerFixture(t)
	service := &testService{running: true}
	f.installer.Service = service
	f.installer.Health = func(context.Context, string) error {
		service.stopErr = errors.New("candidate termination unknown")
		return errors.New("unhealthy")
	}
	if err := f.installer.Install(context.Background(), f.request("40112233-4455-6677-8899-aabbccddeeff")); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal(err)
	}
	state, _ := f.installer.readState()
	if state.State != "rollback-intent" {
		t.Fatalf("%+v", state)
	}
	target, _ := os.Readlink(f.active)
	if target != state.Candidate {
		t.Fatal("switched old binary without stopping candidate")
	}
	if f.restored != nil {
		t.Fatal("restored settings without stop witness")
	}
}

func TestServiceInstallerJournalLockAcrossInstances(t *testing.T) {
	f := newInstallerFixture(t)
	unlock, err := f.installer.lockJournal()
	if err != nil {
		t.Fatal(err)
	}
	other := &Installer{Root: f.root, ActiveLink: f.active, PublicKey: f.installer.PublicKey, DrainTimeout: f.installer.DrainTimeout, HealthTimeout: f.installer.HealthTimeout, CheckCompatibility: f.installer.CheckCompatibility, SnapshotCompatible: f.installer.SnapshotCompatible, RestoreCompatible: f.installer.RestoreCompatible, Drain: f.installer.Drain, Health: f.installer.Health, IrreversibleMigration: f.installer.IrreversibleMigration}
	if err = other.Install(context.Background(), f.request("50112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("second installer mutated journal")
	}
	unlock()
	if err = other.Install(context.Background(), f.request("50112233-4455-6677-8899-aabbccddeeff")); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStaleActiveLinkCannotBeOverwritten(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Failpoint = func(stage string) error {
		if stage == "before-activate" {
			if err := os.Remove(f.active); err != nil {
				return err
			}
			return os.Symlink("releases/other/vortex-node", f.active)
		}
		return nil
	}
	if err := f.installer.Install(context.Background(), f.request("60112233-4455-6677-8899-aabbccddeeff")); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal(err)
	}
	target, _ := os.Readlink(f.active)
	if target != "releases/other/vortex-node" {
		t.Fatal("stale installer replaced current selection")
	}
}
