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

func TestSystemdServiceRejectsCommandInjectionAndUnselectedRelease(t *testing.T) {
	for _, unit := range []string{"--root=/tmp/x.service", "node;touch.service", "node\n.service", "../node.service", ""} {
		s := &SystemdService{Unit: unit, ActiveLink: "/tmp/current"}
		if err := s.validate(); err == nil {
			t.Fatalf("accepted unit %q", unit)
		}
	}
	root := t.TempDir()
	selected := filepath.Join(root, "selected")
	other := filepath.Join(root, "other")
	os.WriteFile(selected, []byte("selected"), 0700)
	os.WriteFile(other, []byte("other"), 0700)
	active := filepath.Join(root, "current")
	os.Symlink(selected, active)
	s := &SystemdService{Unit: "vortex-node.service", ActiveLink: active}
	if err := s.Start(context.Background(), other); err == nil {
		t.Fatal("unselected release reached service manager")
	}
}

func TestForwardingProbeCannotUseQualificationOrInventEmptyRule(t *testing.T) {
	for _, p := range []ForwardingProbe{{}, {Request: []byte("health"), Response: []byte("health")}} {
		if err := p.Check(context.Background()); err == nil {
			t.Fatal("invalid probe reached network")
		}
	}
}

func TestServiceReleaseTargetCannotEscapeThroughDirectorySymlink(t *testing.T) {
	f := newInstallerFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.root, "releases", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.installer.resolveTarget("releases/escape/vortex-node"); err == nil {
		t.Fatal("release target escaped trusted installer root")
	}
}

func TestServiceCrashAfterSelectionRecoversSameTransactionWithFreshWitness(t *testing.T) {
	f := newInstallerFixture(t)
	service := &testService{running: true}
	f.installer.Service = service
	f.installer.Failpoint = func(stage string) error {
		if stage == "after-activate" {
			return errors.New("upgrader crashed")
		}
		return nil
	}
	request := f.request("80112233-4455-6677-8899-aabbccddeeff")
	if err := f.installer.Install(context.Background(), request); err == nil {
		t.Fatal("crash ignored")
	}
	state, err := f.installer.State()
	if err != nil || state.State != "activation-intent" {
		t.Fatalf("%+v %v", state, err)
	}
	f.installer.Failpoint = nil
	if err = f.installer.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = f.installer.State()
	if err != nil || state.State != "healthy" || state.TransactionID != request.TransactionID {
		t.Fatalf("%+v %v", state, err)
	}
	if got := strings.Join(service.events, ","); got != "stop,stop,start:80112233-4455-6677-8899-aabbccddeeff" {
		t.Fatal(got)
	}
}

func TestServiceRecoveredCandidateDigestCannotBeChanged(t *testing.T) {
	f := newInstallerFixture(t)
	service := &testService{running: true}
	f.installer.Service = service
	f.installer.Failpoint = func(stage string) error {
		if stage == "after-activate" {
			return errors.New("crash")
		}
		return nil
	}
	request := f.request("90112233-4455-6677-8899-aabbccddeeff")
	if err := f.installer.Install(context.Background(), request); err == nil {
		t.Fatal("crash ignored")
	}
	state, _ := f.installer.State()
	path := filepath.Join(f.root, state.Candidate)
	os.WriteFile(path, []byte("changed"), 0755)
	if err := f.installer.Recover(context.Background()); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal(err)
	}
	if len(service.events) != 1 {
		t.Fatal("unverified recovered artifact reached service")
	}
}

func TestServiceFullDiskCannotStopOrSelectCandidate(t *testing.T) {
	base := os.Getenv("VORTEX_TEST_FULL_DISK_DIR")
	if base == "" {
		t.Skip("isolated Linux full-disk fixture required by deployment gate")
	}
	root, err := os.MkdirTemp(base, "upgrade-full-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	f := newInstallerFixture(t)
	old := filepath.Join(root, "releases/old/vortex-node")
	os.MkdirAll(filepath.Dir(old), 0700)
	if err = os.WriteFile(old, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(root, "current")
	if err = os.Symlink("releases/old/vortex-node", active); err != nil {
		t.Fatal(err)
	}
	f.installer.Root, f.installer.ActiveLink = root, active
	service := &testService{running: true}
	f.installer.Service = service
	filler, err := os.Create(filepath.Join(root, "fill"))
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 65536)
	for i := 0; i < 512; i++ {
		_, err = filler.Write(chunk)
		if err != nil {
			break
		}
	}
	filler.Close()
	if err == nil {
		t.Fatal("fixture was not a bounded full filesystem")
	}
	if err = f.installer.Install(context.Background(), f.request("a0112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("full disk upgrade succeeded")
	}
	if len(service.events) != 0 {
		t.Fatal("stopped service before durable staging")
	}
	target, err := os.Readlink(active)
	if err != nil || target != "releases/old/vortex-node" {
		t.Fatal("disk failure switched release")
	}
}

func TestServiceCrashBoundariesRetainOneTransaction(t *testing.T) {
	for _, stage := range []string{"before-stage", "staged", "before-drain", "after-drain", "after-service-stop", "before-activate", "after-activate", "before-health", "healthy"} {
		t.Run(stage, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.installer.Service = &testService{running: true}
			request := f.request("b0112233-4455-6677-8899-aabbccddeeff")
			f.installer.Failpoint = func(current string) error {
				if current == stage {
					return errors.New("simulated upgrader crash")
				}
				return nil
			}
			if err := f.installer.Install(context.Background(), request); err == nil {
				t.Fatal("missing failpoint")
			}
			state, err := f.installer.State()
			if err != nil || state.TransactionID != request.TransactionID {
				t.Fatalf("lost durable transaction %+v %v", state, err)
			}
			f.installer.Failpoint = nil
			// An unelapsed drain remains pending, not activation proof.
			err = f.installer.Recover(context.Background())
			if err != nil && !errors.Is(err, ErrActivationUnknown) {
				t.Fatal(err)
			}
			state, err = f.installer.State()
			if err != nil || state.TransactionID != request.TransactionID {
				t.Fatal("recovery changed identity")
			}
		})
	}
}
