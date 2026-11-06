package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrActivationUnknown = errors.New("upgrade: activation outcome unknown")
	ErrNotActivated      = errors.New("upgrade: candidate was not activated")
	ErrRollbackBlocked   = errors.New("upgrade: irreversible migration blocks rollback")
)

type InstallRequest struct {
	TransactionID string
	Artifact      []byte
	Signature     []byte
	SHA256        [sha256.Size]byte
}

type InstallState struct {
	SchemaVersion int       `json:"schema_version"`
	TransactionID string    `json:"transaction_id"`
	State         string    `json:"state"`
	Previous      string    `json:"previous_target"`
	Candidate     string    `json:"candidate_target"`
	SHA256        string    `json:"sha256"`
	Snapshot      string    `json:"snapshot_path"`
	DrainDeadline time.Time `json:"drain_deadline,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	Reason        string    `json:"reason,omitempty"`
}

type Installer struct {
	Service               ServiceController
	mu                    sync.Mutex
	Root                  string
	ActiveLink            string
	PublicKey             ed25519.PublicKey
	DrainTimeout          time.Duration
	HealthTimeout         time.Duration
	ReservedDiskBytes     uint64
	CheckCompatibility    func(context.Context, []byte) error
	SnapshotCompatible    func(context.Context) ([]byte, error)
	RestoreCompatible     func(context.Context, []byte) error
	Drain                 func(context.Context) error
	Health                func(context.Context, string) error
	IrreversibleMigration func(context.Context) (bool, error)
	Failpoint             func(string) error
}

func (i *Installer) Install(ctx context.Context, request InstallRequest) error {
	if i == nil {
		return fmt.Errorf("upgrade: installer configuration is incomplete")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	unlock, err := i.lockJournal()
	if err != nil {
		return err
	}
	defer unlock()
	return i.installLocked(ctx, request)
}

func (i *Installer) installLocked(ctx context.Context, request InstallRequest) error {
	if err := i.validate(); err != nil {
		return err
	}
	if !validTransactionID(request.TransactionID) || len(request.Artifact) == 0 || len(request.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("upgrade: transaction, artifact and signature are required")
	}
	digest := sha256.Sum256(request.Artifact)
	if subtle.ConstantTimeCompare(digest[:], request.SHA256[:]) != 1 || !ed25519.Verify(i.PublicKey, request.Artifact, request.Signature) {
		return fmt.Errorf("upgrade: artifact digest or signature invalid")
	}
	if err := i.CheckCompatibility(ctx, request.Artifact); err != nil {
		return fmt.Errorf("upgrade: compatibility preflight: %w", err)
	}
	var priorState *InstallState
	if existing, err := i.readState(); err == nil {
		if existing.TransactionID == request.TransactionID {
			if existing.SHA256 != hex.EncodeToString(digest[:]) {
				return fmt.Errorf("upgrade: transaction ID reused for a different artifact")
			}
			switch existing.State {
			case "healthy":
				if i.Service != nil {
					path, err := i.resolveTarget(existing.Candidate)
					if err != nil {
						return err
					}
					health, cancel := context.WithTimeout(ctx, i.HealthTimeout)
					defer cancel()
					if err = i.Health(health, path); err != nil {
						return errors.Join(ErrActivationUnknown, err)
					}
				}
				return nil
			case "rolled-back", "rollback-blocked":
				return fmt.Errorf("upgrade: transaction already ended in %s", existing.State)
			case "activation-intent", "activation-unknown", "health-check", "rollback-intent", "process-stop-intent", "process-stop-unknown":
				if err := i.recoverLocked(ctx); err != nil {
					return err
				}
				existing, err = i.readState()
				if err != nil {
					return err
				}
				if existing.State == "activation-unknown" || existing.State == "health-check" || existing.State == "rollback-blocked" {
					return ErrActivationUnknown
				}
			case "draining":
				if err := i.recoverLocked(ctx); err != nil {
					return err
				}
				existing, err = i.readState()
				if err != nil {
					return err
				}
				if existing.State == "draining" {
					return fmt.Errorf("upgrade: prior drain deadline has not elapsed")
				}
			case "staging", "staged", "not-activated":
				if err := i.recoverLocked(ctx); err != nil {
					return err
				}
				existing, err = i.readState()
				if err != nil {
					return err
				}
				if existing.State == "activation-unknown" {
					return ErrActivationUnknown
				}
			}
		} else {
			switch existing.State {
			case "healthy", "rolled-back", "not-activated":
				priorState = &existing
			default:
				return fmt.Errorf("upgrade: another transaction is unresolved")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	previous, err := i.activeTarget()
	if err != nil {
		return err
	}
	previousPath, err := i.resolveTarget(previous)
	if err != nil {
		return err
	}
	previousInfo, err := os.Stat(previousPath)
	if err != nil || !previousInfo.Mode().IsRegular() || previousInfo.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("upgrade: current release is missing or not executable")
	}
	snapshot, err := i.SnapshotCompatible(ctx)
	if err != nil {
		return fmt.Errorf("upgrade: compatible state snapshot: %w", err)
	}
	if err := i.checkDisk(uint64(len(request.Artifact)) + uint64(len(snapshot)) + uint64(previousInfo.Size())); err != nil {
		return err
	}
	candidate := filepath.ToSlash(filepath.Join("releases", request.TransactionID, "vortex-node"))
	candidatePath, err := i.resolveTarget(candidate)
	if err != nil {
		return err
	}
	snapshotPath := filepath.Join(i.Root, "transactions", request.TransactionID+".compatible-state")
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o700); err != nil {
		return err
	}
	if err := writeSyncedFile(snapshotPath, snapshot, 0o600); err != nil {
		return fmt.Errorf("upgrade: persist compatible snapshot: %w", err)
	}
	if priorState != nil {
		if err := i.archiveState(*priorState); err != nil {
			return err
		}
	}
	state := InstallState{SchemaVersion: 1, TransactionID: request.TransactionID, State: "staging", Previous: previous, Candidate: candidate, SHA256: hex.EncodeToString(digest[:]), Snapshot: snapshotPath}
	if err := i.writeState(state); err != nil {
		return err
	}
	if err := i.fail("before-stage"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o755); err != nil {
		return err
	}
	if err := writeSyncedFile(candidatePath, request.Artifact, 0o755); err != nil {
		return fmt.Errorf("upgrade: stage artifact: %w", err)
	}
	if err := syncDirectory(filepath.Dir(candidatePath)); err != nil {
		return err
	}
	state.State = "staged"
	if err := i.writeState(state); err != nil {
		return err
	}
	if err := i.fail("staged"); err != nil {
		return err
	}
	if err := i.fail("before-drain"); err != nil {
		return err
	}
	state.State = "draining"
	state.DrainDeadline = time.Now().UTC().Add(i.DrainTimeout)
	if err := i.writeState(state); err != nil {
		return err
	}
	drainCtx, cancelDrain := context.WithDeadline(ctx, state.DrainDeadline.Add(15*time.Second))
	drainErr := i.Drain(drainCtx)
	cancelDrain()
	if drainErr != nil {
		state.State, state.Reason = "not-activated", "drain_failed"
		_ = i.writeState(state)
		return fmt.Errorf("upgrade: bounded drain: %w", drainErr)
	}
	if err := i.fail("after-drain"); err != nil {
		return err
	}
	if i.Service != nil {
		state.State = "process-stop-intent"
		if err := i.writeState(state); err != nil {
			return err
		}
		if err := i.stopService(ctx); err != nil {
			state.State, state.Reason = "process-stop-unknown", "termination_unproven"
			return errors.Join(err, i.writeState(state))
		}
		if err := i.fail("after-service-stop"); err != nil {
			return err
		}
	}
	state.State = "activation-intent"
	if err := i.writeState(state); err != nil {
		return err
	}
	if err := i.fail("before-activate"); err != nil {
		return err
	}
	if current, err := i.activeTarget(); err != nil || current != previous {
		state.State, state.Reason = "activation-unknown", "active_target_changed_before_switch"
		return errors.Join(ErrActivationUnknown, err, i.writeState(state))
	}
	if err := i.atomicActivate(candidate); err != nil {
		state.State, state.Reason = "not-activated", "activation_failed"
		_ = i.writeState(state)
		return err
	}
	if err := i.fail("after-activate"); err != nil {
		return err
	}
	state.State = "health-check"
	if err := i.writeState(state); err != nil {
		return errors.Join(ErrActivationUnknown, err)
	}
	if err := i.fail("before-health"); err != nil {
		return err
	}
	healthCtx, cancelHealth := context.WithTimeout(ctx, i.HealthTimeout)
	healthErr := i.startAndCheck(healthCtx, candidatePath)
	cancelHealth()
	if healthErr == nil {
		state.State, state.Reason = "healthy", ""
		if err := i.writeState(state); err != nil {
			return errors.Join(ErrActivationUnknown, err)
		}
		return i.fail("healthy")
	}
	return i.rollback(ctx, state, healthErr)
}

// Recover derives activation only from the durable transaction and the atomic
// active-link target. Process exit and elapsed time are never activation proof.
func (i *Installer) Recover(ctx context.Context) error {
	if i == nil {
		return fmt.Errorf("upgrade: installer configuration is incomplete")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	unlock, err := i.lockJournal()
	if err != nil {
		return err
	}
	defer unlock()
	return i.recoverLocked(ctx)
}

func (i *Installer) recoverLocked(ctx context.Context) error {
	if err := i.validate(); err != nil {
		return err
	}
	state, err := i.readState()
	if err != nil {
		return err
	}
	active, err := i.activeTarget()
	if err != nil {
		return err
	}
	switch state.State {
	case "healthy":
		if active != state.Candidate {
			state.State = "activation-unknown"
			_ = i.writeState(state)
			return ErrActivationUnknown
		}
		return nil
	case "process-stop-intent", "process-stop-unknown":
		if i.Service == nil || active != state.Previous {
			return ErrActivationUnknown
		}
		if err := i.stopService(ctx); err != nil {
			return err
		}
		state.State, state.Reason = "not-activated", "service_termination_proven"
		return i.writeState(state)
	case "rolled-back", "not-activated", "staging", "staged", "draining":
		if active != state.Previous {
			state.State = "activation-unknown"
			_ = i.writeState(state)
			return ErrActivationUnknown
		}
		if state.State == "draining" && !state.DrainDeadline.IsZero() && !time.Now().Before(state.DrainDeadline) {
			state.State, state.Reason = "not-activated", "drain_deadline_elapsed"
			return i.writeState(state)
		}
		return nil
	case "activation-intent", "activation-unknown", "health-check":
		if active == state.Candidate {
			state.State = "activation-unknown"
			if err := i.writeState(state); err != nil {
				return errors.Join(ErrActivationUnknown, err)
			}
			return ErrActivationUnknown
		}
		if active == state.Previous {
			state.State = "not-activated"
			state.Reason = "active_link_proves_candidate_not_selected"
			return i.writeState(state)
		}
		return ErrActivationUnknown
	case "rollback-intent":
		if active == state.Previous {
			if i.Service != nil {
				path, err := i.resolveTarget(state.Previous)
				if err != nil {
					return err
				}
				health, cancel := context.WithTimeout(ctx, i.HealthTimeout)
				defer cancel()
				if err = i.startAndCheck(health, path); err != nil {
					return errors.Join(ErrActivationUnknown, err)
				}
			}
			state.State, state.Reason = "rolled-back", ""
			return i.writeState(state)
		}
		if active != state.Candidate {
			return ErrActivationUnknown
		}
		if err := i.completeRollback(ctx, state, errors.New("recovered rollback intent")); err != nil {
			if recovered, readErr := i.readState(); readErr == nil && recovered.State == "rolled-back" {
				return nil
			}
			return err
		}
		return nil
	case "rollback-blocked":
		return ErrRollbackBlocked
	default:
		return fmt.Errorf("upgrade: unknown durable state %q", state.State)
	}
}

func (i *Installer) rollback(ctx context.Context, state InstallState, healthErr error) error {
	irreversible, err := i.IrreversibleMigration(ctx)
	if err != nil || irreversible {
		state.State = "rollback-blocked"
		state.Reason = "irreversible_migration_or_state_unknown"
		_ = i.writeState(state)
		return errors.Join(ErrRollbackBlocked, healthErr, err)
	}
	return i.completeRollback(ctx, state, healthErr)
}

func (i *Installer) completeRollback(ctx context.Context, state InstallState, cause error) error {
	state.State = "rollback-intent"
	if err := i.writeState(state); err != nil {
		return errors.Join(ErrActivationUnknown, cause, err)
	}
	if err := i.fail("before-rollback-state"); err != nil {
		return err
	}
	if err := i.stopService(ctx); err != nil {
		return errors.Join(ErrActivationUnknown, cause, err)
	}
	snapshot, err := os.ReadFile(state.Snapshot)
	if err != nil {
		return errors.Join(ErrActivationUnknown, cause, err)
	}
	if err := i.RestoreCompatible(ctx, snapshot); err != nil {
		return errors.Join(ErrActivationUnknown, cause, fmt.Errorf("upgrade: restore compatible state: %w", err))
	}
	if err := i.fail("after-rollback-state"); err != nil {
		return err
	}
	if err := i.atomicActivate(state.Previous); err != nil {
		return errors.Join(ErrActivationUnknown, cause, err)
	}
	if i.Service != nil {
		path, err := i.resolveTarget(state.Previous)
		if err != nil {
			return err
		}
		health, cancel := context.WithTimeout(ctx, i.HealthTimeout)
		defer cancel()
		if err = i.startAndCheck(health, path); err != nil {
			return errors.Join(ErrActivationUnknown, cause, err)
		}
	}
	state.State, state.Reason = "rolled-back", "health_check_failed"
	if err := i.writeState(state); err != nil {
		return errors.Join(ErrActivationUnknown, cause, err)
	}
	return fmt.Errorf("upgrade: health check failed; previous release restored: %w", cause)
}

func (i *Installer) atomicActivate(target string) error {
	if _, err := i.resolveTarget(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(i.ActiveLink), 0o755); err != nil {
		return err
	}
	temporary := i.ActiveLink + ".next"
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return fmt.Errorf("upgrade: stage active-link switch: %w", err)
	}
	if err := syncDirectory(filepath.Dir(i.ActiveLink)); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, i.ActiveLink); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("upgrade: atomic active-link switch: %w", err)
	}
	return syncDirectory(filepath.Dir(i.ActiveLink))
}

func (i *Installer) checkDisk(required uint64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(i.Root, &stat); err != nil {
		return fmt.Errorf("upgrade: inspect free disk space: %w", err)
	}
	available := stat.Bavail * uint64(stat.Bsize)
	if required > ^uint64(0)-i.ReservedDiskBytes {
		return fmt.Errorf("upgrade: requested disk reserve overflows")
	}
	required += i.ReservedDiskBytes
	if available < required {
		return fmt.Errorf("upgrade: insufficient free disk space: need %d bytes, have %d", required, available)
	}
	return nil
}

func (i *Installer) validate() error {
	if i == nil || i.Root == "" || i.ActiveLink == "" || len(i.PublicKey) != ed25519.PublicKeySize || i.DrainTimeout <= 0 || i.HealthTimeout <= 0 || i.CheckCompatibility == nil || i.SnapshotCompatible == nil || i.RestoreCompatible == nil || i.Drain == nil || i.Health == nil || i.IrreversibleMigration == nil {
		return fmt.Errorf("upgrade: installer configuration is incomplete")
	}
	root, err := filepath.Abs(i.Root)
	if err != nil {
		return err
	}
	active, err := filepath.Abs(i.ActiveLink)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, active)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("upgrade: active link must be inside the upgrade root")
	}
	i.Root, i.ActiveLink = root, active
	return os.MkdirAll(i.Root, 0o755)
}

func (i *Installer) activeTarget() (string, error) {
	target, err := os.Readlink(i.ActiveLink)
	if err != nil {
		return "", fmt.Errorf("upgrade: current release link unavailable: %w", err)
	}
	if _, err := i.resolveTarget(target); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(target)), nil
}

func (i *Installer) resolveTarget(target string) (string, error) {
	if target == "" || filepath.IsAbs(target) {
		return "", fmt.Errorf("upgrade: release target must be a non-empty relative path")
	}
	root, err := filepath.Abs(i.Root)
	if err != nil {
		return "", err
	}
	resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(target)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("upgrade: release target escapes upgrade root")
	}
	// Release and transaction directories are installer-owned. Reject nested
	// symlinks rather than permitting an existing target to escape lexically valid paths.
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("upgrade: symlink inside release target")
		}
	}
	return resolved, nil
}

func (i *Installer) statePath() string { return filepath.Join(i.Root, "transaction.json") }

func (i *Installer) readState() (InstallState, error) {
	data, err := os.ReadFile(i.statePath())
	if err != nil {
		return InstallState{}, err
	}
	var state InstallState
	if err := json.Unmarshal(data, &state); err != nil {
		return InstallState{}, fmt.Errorf("upgrade: parse transaction state: %w", err)
	}
	if state.SchemaVersion != 1 || !validTransactionID(state.TransactionID) {
		return InstallState{}, fmt.Errorf("upgrade: transaction state identity invalid")
	}
	return state, nil
}

func (i *Installer) writeState(state InstallState) error {
	state.SchemaVersion = 1
	state.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeSyncedFile(i.statePath(), data, 0o600); err != nil {
		return fmt.Errorf("upgrade: persist transaction state: %w", err)
	}
	return syncDirectory(i.Root)
}

func (i *Installer) archiveState(state InstallState) error {
	path := filepath.Join(i.Root, "transactions", state.TransactionID+".json")
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeSyncedFile(path, data, 0o600); err != nil {
		return fmt.Errorf("upgrade: archive prior transaction: %w", err)
	}
	return nil
}

func writeSyncedFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".vortex-upgrade-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (i *Installer) fail(stage string) error {
	if i.Failpoint == nil {
		return nil
	}
	return i.Failpoint(stage)
}

func validTransactionID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
