package main

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"vortex/agent"
	"vortex/agent/store"
)

func runCommand(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	command := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	} else {
		fmt.Fprintln(diagnostics, "deprecated: use 'vortex-node run' explicitly")
	}
	switch command {
	case "run":
		return runNode(ctx, args, out, diagnostics)
	case "enroll":
		return enrollNode(ctx, args, out, diagnostics)
	case "status", "drain":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		flags.SetOutput(diagnostics)
		socket := flags.String("admin-socket", "", "private node Unix socket")
		database := flags.String("db", "agent.db", "database identity for default socket; never opened by status/drain")
		timeout := flags.Duration("drain-timeout", 30*time.Second, "bounded drain duration")
		if err := flags.Parse(args); err != nil {
			return err
		}
		if flags.NArg() != 0 || *timeout <= 0 || *timeout > 5*time.Minute {
			return errors.New("invalid command arguments or drain timeout")
		}
		if *socket == "" {
			var err error
			*socket, err = agent.DefaultAdminSocket(*database)
			if err != nil {
				return err
			}
		}
		callCtx, cancel := context.WithTimeout(ctx, *timeout+15*time.Second)
		defer cancel()
		status, err := agent.AdminCall(callCtx, *socket, command, *timeout)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(status)
	default:
		return errors.New("unknown command: expected enroll, run, status or drain")
	}
}
func enrollNode(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("enroll", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	node := flags.String("node-id", "", "node UUID")
	database := flags.String("db", "agent.db", "Agent SQLite path")
	directory := flags.String("credential-dir", "credentials", "private credential directory")
	endpoint := flags.String("enrollment-hub", "", "trusted enrollment endpoint")
	name := flags.String("hub-name", "", "expected issuer DNS")
	ca := flags.String("tls-ca", "", "trusted enrollment CA PEM")
	tokenFile := flags.String("token-file", "", "private local one-time token file; alternatively VORTEX_ENROLLMENT_TOKEN")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *node == "" || *endpoint == "" || *name == "" || *ca == "" {
		return errors.New("enroll requires node-id, enrollment-hub, hub-name and tls-ca")
	}
	token := os.Getenv("VORTEX_ENROLLMENT_TOKEN")
	if *tokenFile != "" {
		info, err := os.Lstat(*tokenFile)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
			return errors.New("token file must be a bounded private regular file")
		}
		raw, err := os.ReadFile(*tokenFile)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(raw))
	}
	if token == "" || len(token) > 4096 {
		return errors.New("private enrollment token required")
	}
	trust, err := os.ReadFile(*ca)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return errors.New("invalid enrollment CA")
	}
	unlock, err := agent.LockProcess(*database)
	if err != nil {
		return err
	}
	defer unlock()
	db, err := store.Open(*database, "agent")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, _, err = db.Credential(ctx, *node); err == nil {
		return errors.New("node is already enrolled")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return errors.New("cannot inspect existing enrollment")
	}
	absolute, err := filepath.Abs(*directory)
	if err != nil {
		return err
	}
	credential, err := agent.Register(ctx, *node, token, *endpoint, absolute, roots, *name, db)
	if err != nil {
		return errors.New("node enrollment failed")
	}
	return json.NewEncoder(out).Encode(map[string]string{"node_id": credential.NodeID, "expires_at": credential.Leaf.NotAfter.UTC().Format(time.RFC3339)})
}
