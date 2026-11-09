package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/agent/upgrade"
)

// The preflight operation never creates/migrates the live database. Only this
// explicit schema generation is eligible for automatic rollback by this binary.
func upgradePreflight(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("upgrade-preflight", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	database := flags.String("db", "", "existing Agent database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *database == "" {
		return errors.New("existing db required")
	}
	db, err := store.OpenReader(*database, "agent")
	if err != nil {
		return err
	}
	defer db.Close()
	schema, err := db.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if schema != 20 {
		return errors.New("upgrade: schema requires separate migration review")
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema": schema, "read_only": true, "automatic_rollback": true})
}
func readUpgradeFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, errors.New("upgrade: invalid bounded input file")
	}
	return os.ReadFile(path)
}
func upgradeNode(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	root := flags.String("root", "", "private installer root")
	active := flags.String("active-link", "", "installed relative release selector")
	database := flags.String("db", "", "existing Agent database (read only preflight)")
	socket := flags.String("admin-socket", "", "private running node socket")
	unit := flags.String("unit", "vortex-node.service", "systemd node unit")
	artifactPath := flags.String("artifact", "", "signed candidate binary")
	signaturePath := flags.String("signature", "", "raw detached Ed25519 signature")
	publicPath := flags.String("public-key", "", "pinned raw Ed25519 public key")
	expectedSHA := flags.String("sha256", "", "expected artifact SHA256")
	transaction := flags.String("transaction-id", "", "stable transaction UUID")
	probePath := flags.String("probe", "", "private authorized forwarding probe JSON")
	drain := flags.Duration("drain-timeout", 30*time.Second, "bounded drain")
	health := flags.Duration("health-timeout", 20*time.Second, "bounded ready and forwarding check")
	recoverOnly := flags.Bool("recover", false, "query/reconcile same durable transaction; never issue a new grant")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*root) || !filepath.IsAbs(*active) || !filepath.IsAbs(*database) || !filepath.IsAbs(*socket) || *health <= 0 || *health > 5*time.Minute || *drain <= 0 || *drain > 5*time.Minute {
		return errors.New("upgrade: bounded durations and absolute deployment paths required")
	}
	ctx, cancel := context.WithTimeout(ctx, *drain+2*(*health)+time.Minute)
	defer cancel()
	public, err := readUpgradeFile(*publicPath, ed25519.PublicKeySize)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("upgrade: pinned public key required")
	}
	raw, err := readUpgradeFile(*probePath, 65536)
	if err != nil {
		return err
	}
	var probe upgrade.ForwardingProbe
	if err = json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	// This snapshot is observations only: never DB/WAL/credentials/fencing/grants.
	before, err := agent.AdminCall(ctx, *socket, "status", time.Second)
	if err != nil {
		if !*recoverOnly {
			return err
		}
		before = agent.NodeStatus{NodeID: probe.Peer.NextHopNodeID}
	}
	service := &upgrade.SystemdService{Unit: *unit, ActiveLink: *active}
	installer := &upgrade.Installer{Root: *root, ActiveLink: *active, PublicKey: public, DrainTimeout: *drain, HealthTimeout: *health, ReservedDiskBytes: 16 << 20, Service: service}
	preflight := func(ctx context.Context, path string) error {
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		command := exec.CommandContext(checkCtx, path, "upgrade-preflight", "--db", *database)
		result, err := command.Output()
		if err != nil {
			return errors.New("upgrade: signed release cannot read current schema")
		}
		var report struct {
			Schema   uint64 `json:"schema"`
			ReadOnly bool   `json:"read_only"`
			Rollback bool   `json:"automatic_rollback"`
		}
		if len(result) > 1024 || json.Unmarshal(result, &report) != nil || report.Schema != 16 || !report.ReadOnly || !report.Rollback {
			return errors.New("upgrade: incompatible rollback schema")
		}
		return nil
	}
	installer.CheckCompatibility = func(ctx context.Context, artifact []byte) error {
		if err := preflight(ctx, *active); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(*root, "preflight-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "candidate")
		if err = os.WriteFile(path, artifact, 0700); err != nil {
			return err
		}
		return preflight(ctx, path)
	}
	installer.SnapshotCompatible = func(context.Context) ([]byte, error) { return []byte(`{"kind":"no-settings-changed","schema":1}`), nil }
	installer.RestoreCompatible = func(_ context.Context, snapshot []byte) error {
		if string(snapshot) != `{"kind":"no-settings-changed","schema":1}` {
			return errors.New("upgrade: unapproved compatibility snapshot")
		}
		return nil
	}
	installer.IrreversibleMigration = func(ctx context.Context) (bool, error) { err := preflight(ctx, *active); return err != nil, err }
	installer.Drain = func(ctx context.Context) error { _, err := agent.AdminCall(ctx, *socket, "drain", *drain); return err }
	installer.Health = func(ctx context.Context, path string) error {
		expected, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(expected)
		for {
			status, err := agent.AdminCall(ctx, *socket, "status", time.Second)
			if err == nil && status.BootID != before.BootID && status.NodeID == before.NodeID && status.HubMode == "online" && status.AppliedVersion != "0" && status.DrainState == "running" && status.BuildSHA256 == hex.EncodeToString(sum[:]) && status.DurableStore {
				if err = probe.Check(ctx); err == nil {
					return nil
				}
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("upgrade: current ready/authority/traffic not proven: %w", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err = os.MkdirAll(*root, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(*root); err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("upgrade: private installer root required")
	}
	if *recoverOnly {
		err = installer.Recover(ctx)
	} else {
		artifact, readErr := readUpgradeFile(*artifactPath, 256<<20)
		if readErr != nil {
			return readErr
		}
		signature, readErr := readUpgradeFile(*signaturePath, ed25519.SignatureSize)
		if readErr != nil {
			return readErr
		}
		digest, readErr := hex.DecodeString(*expectedSHA)
		if readErr != nil || len(digest) != 32 {
			return errors.New("upgrade: exact SHA256 required")
		}
		var sum [32]byte
		copy(sum[:], digest)
		err = installer.Install(ctx, upgrade.InstallRequest{TransactionID: *transaction, Artifact: artifact, Signature: signature, SHA256: sum})
	}
	if err != nil {
		return err
	}
	state, err := installer.State()
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]string{"status": state.State, "transaction_id": state.TransactionID, "scope": "node-service-upgrade"})
}
