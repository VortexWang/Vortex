package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type installerFixture struct {
	root      string
	active    string
	artifact  []byte
	private   ed25519.PrivateKey
	installer *Installer
	restored  []byte
}

func newInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	root := t.TempDir()
	oldRelease := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(oldRelease, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRelease, "vortex-node"), []byte("old-release"), 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(root, "current")
	if err := os.Symlink("releases/old/vortex-node", active); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &installerFixture{
		root: root, active: active, artifact: []byte("new-release"), private: private,
	}
	fixture.installer = &Installer{
		Root: root, ActiveLink: active, PublicKey: public,
		DrainTimeout: time.Second, HealthTimeout: time.Second,
		CheckCompatibility: func(context.Context, []byte) error { return nil },
		SnapshotCompatible: func(context.Context) ([]byte, error) { return []byte("compatible-state"), nil },
		RestoreCompatible: func(_ context.Context, data []byte) error {
			fixture.restored = append([]byte(nil), data...)
			return nil
		},
		Drain: func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("drain context has no deadline")
			}
			return nil
		},
		Health:                func(context.Context, string) error { return nil },
		IrreversibleMigration: func(context.Context) (bool, error) { return false, nil },
	}
	return fixture
}

func (f *installerFixture) request(transactionID string) InstallRequest {
	digest := sha256.Sum256(f.artifact)
	return InstallRequest{
		TransactionID: transactionID,
		Artifact:      append([]byte(nil), f.artifact...),
		Signature:     ed25519.Sign(f.private, f.artifact),
		SHA256:        digest,
	}
}

func TestInstallerStagesAndAtomicallyActivatesSignedArtifact(t *testing.T) {
	f := newInstallerFixture(t)
	request := f.request("00112233-4455-6677-8899-aabbccddeeff")
	if err := f.installer.Install(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	active, err := os.Readlink(f.active)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(active)))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(f.artifact) {
		t.Fatalf("active artifact = %q, want %q", installed, f.artifact)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "healthy" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerRejectsDigestOrSignatureBeforeChangingActiveBinary(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*InstallRequest)
	}{
		{name: "digest", change: func(request *InstallRequest) { request.SHA256[0] ^= 0xff }},
		{name: "signature", change: func(request *InstallRequest) { request.Signature[0] ^= 0xff }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newInstallerFixture(t)
			request := f.request("10112233-4455-6677-8899-aabbccddeeff")
			test.change(&request)
			if err := f.installer.Install(context.Background(), request); err == nil {
				t.Fatal("invalid artifact was accepted")
			}
			active, err := os.Readlink(f.active)
			if err != nil || active != "releases/old/vortex-node" {
				t.Fatalf("active=%q err=%v", active, err)
			}
		})
	}
}

func TestInstallerHealthFailureRestoresStateAndPreviousBinary(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Health = func(context.Context, string) error { return errors.New("unhealthy") }
	if err := f.installer.Install(context.Background(), f.request("20112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("health failure was not returned")
	}
	active, err := os.Readlink(f.active)
	if err != nil || active != "releases/old/vortex-node" {
		t.Fatalf("active=%q err=%v", active, err)
	}
	if string(f.restored) != "compatible-state" {
		t.Fatalf("restored state = %q", f.restored)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "rolled-back" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerIrreversibleMigrationBlocksRollback(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Health = func(context.Context, string) error { return errors.New("unhealthy") }
	f.installer.IrreversibleMigration = func(context.Context) (bool, error) { return true, nil }
	if err := f.installer.Install(context.Background(), f.request("30112233-4455-6677-8899-aabbccddeeff")); !errors.Is(err, ErrRollbackBlocked) {
		t.Fatalf("Install() error = %v, want ErrRollbackBlocked", err)
	}
	active, err := os.Readlink(f.active)
	if err != nil || active != "releases/30112233-4455-6677-8899-aabbccddeeff/vortex-node" {
		t.Fatalf("active=%q err=%v", active, err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "rollback-blocked" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerRestartAfterActivationRetainsUnknownState(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Failpoint = func(stage string) error {
		if stage == "after-activate" {
			return errors.New("simulated process loss")
		}
		return nil
	}
	request := f.request("40112233-4455-6677-8899-aabbccddeeff")
	if err := f.installer.Install(context.Background(), request); err == nil {
		t.Fatal("activation failpoint was not returned")
	}
	active, err := os.Readlink(f.active)
	if err != nil || active != "releases/40112233-4455-6677-8899-aabbccddeeff/vortex-node" {
		t.Fatalf("active=%q err=%v", active, err)
	}
	f.installer.Failpoint = nil
	if err := f.installer.Recover(context.Background()); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("Recover() error = %v, want ErrActivationUnknown", err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "activation-unknown" || state.Previous == "" || state.Candidate == "" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerRecoversPreActivationCrashBoundaries(t *testing.T) {
	for _, stage := range []string{"before-stage", "staged", "after-drain", "before-activate"} {
		t.Run(stage, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.installer.Failpoint = func(current string) error {
				if current == stage {
					return errors.New("simulated process loss")
				}
				return nil
			}
			request := f.request("60112233-4455-6677-8899-aabbccddeeff")
			if err := f.installer.Install(context.Background(), request); err == nil {
				t.Fatal("failpoint was not returned")
			}
			f.installer.Failpoint = nil
			if err := f.installer.Recover(context.Background()); err != nil {
				t.Fatalf("Recover() error = %v", err)
			}
			active, err := os.Readlink(f.active)
			if err != nil || active != "releases/old/vortex-node" {
				t.Fatalf("active=%q err=%v", active, err)
			}
			state, err := f.installer.readState()
			if err != nil {
				t.Fatal(err)
			}
			if stage == "before-activate" && state.State != "not-activated" {
				t.Fatalf("state=%q, want not-activated", state.State)
			}
		})
	}
}

func TestInstallerHealthyTransactionRecoversAgainstCandidate(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Failpoint = func(stage string) error {
		if stage == "healthy" {
			return errors.New("simulated process loss after durable health result")
		}
		return nil
	}
	if err := f.installer.Install(context.Background(), f.request("70112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("healthy failpoint was not returned")
	}
	f.installer.Failpoint = nil
	if err := f.installer.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "healthy" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerCanApplyNextTransactionAndArchivesPreviousState(t *testing.T) {
	f := newInstallerFixture(t)
	firstID := "90112233-4455-6677-8899-aabbccddeeff"
	if err := f.installer.Install(context.Background(), f.request(firstID)); err != nil {
		t.Fatal(err)
	}
	f.artifact = []byte("third-release")
	secondID := "a0112233-4455-6677-8899-aabbccddeeff"
	if err := f.installer.Install(context.Background(), f.request(secondID)); err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(filepath.Join(f.root, "transactions", firstID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var prior InstallState
	if err := json.Unmarshal(archived, &prior); err != nil || prior.TransactionID != firstID || prior.State != "healthy" {
		t.Fatalf("archived state=%+v err=%v", prior, err)
	}
	active, err := os.Readlink(f.active)
	if err != nil || active != "releases/"+secondID+"/vortex-node" {
		t.Fatalf("active=%q err=%v", active, err)
	}
}

func TestInstallerCanRetrySameTransactionAfterStagingCrash(t *testing.T) {
	f := newInstallerFixture(t)
	id := "b0112233-4455-6677-8899-aabbccddeeff"
	request := f.request(id)
	f.installer.Failpoint = func(stage string) error {
		if stage == "staged" {
			return errors.New("simulated process loss")
		}
		return nil
	}
	if err := f.installer.Install(context.Background(), request); err == nil {
		t.Fatal("staged failpoint was not returned")
	}
	f.installer.Failpoint = nil
	if err := f.installer.Install(context.Background(), request); err != nil {
		t.Fatalf("retry after staging crash: %v", err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "healthy" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestInstallerRecoversCrashDuringRollback(t *testing.T) {
	for _, stage := range []string{"before-rollback-state", "after-rollback-state"} {
		t.Run(stage, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.installer.Health = func(context.Context, string) error { return errors.New("unhealthy") }
			f.installer.Failpoint = func(current string) error {
				if current == stage {
					return errors.New("simulated process loss")
				}
				return nil
			}
			if err := f.installer.Install(context.Background(), f.request("c0112233-4455-6677-8899-aabbccddeeff")); err == nil {
				t.Fatal("rollback failpoint was not returned")
			}
			f.installer.Failpoint = nil
			if err := f.installer.Recover(context.Background()); err != nil {
				t.Fatalf("Recover() error = %v", err)
			}
			active, err := os.Readlink(f.active)
			if err != nil || active != "releases/old/vortex-node" {
				t.Fatalf("active=%q err=%v", active, err)
			}
			state, err := f.installer.readState()
			if err != nil || state.State != "rolled-back" {
				t.Fatalf("state=%+v err=%v", state, err)
			}
		})
	}
}

func TestInstallerDiskAndCompatibilityPreflightKeepOldBinary(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Installer)
	}{
		{name: "disk", change: func(installer *Installer) { installer.ReservedDiskBytes = ^uint64(0) }},
		{name: "compatibility", change: func(installer *Installer) {
			installer.CheckCompatibility = func(context.Context, []byte) error { return errors.New("incompatible") }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newInstallerFixture(t)
			test.change(f.installer)
			if err := f.installer.Install(context.Background(), f.request("80112233-4455-6677-8899-aabbccddeeff")); err == nil {
				t.Fatal("preflight failure was not returned")
			}
			active, err := os.Readlink(f.active)
			if err != nil || active != "releases/old/vortex-node" {
				t.Fatalf("active=%q err=%v", active, err)
			}
		})
	}
}

func TestInstallerDrainDeadlineFailureLeavesPreviousBinaryActive(t *testing.T) {
	f := newInstallerFixture(t)
	f.installer.Drain = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	f.installer.DrainTimeout = 20 * time.Millisecond
	if err := f.installer.Install(context.Background(), f.request("50112233-4455-6677-8899-aabbccddeeff")); err == nil {
		t.Fatal("drain timeout was not returned")
	}
	active, err := os.Readlink(f.active)
	if err != nil || active != "releases/old/vortex-node" {
		t.Fatalf("active=%q err=%v", active, err)
	}
	state, err := f.installer.readState()
	if err != nil || state.State != "not-activated" || state.DrainDeadline.IsZero() {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
