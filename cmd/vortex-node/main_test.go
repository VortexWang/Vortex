package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/identity"
	"vortex/agent/store"
	"vortex/hub"
)

func TestCommandDispatchRejectsUnknownAndPrivateTokenLeaks(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if err := runCommand(context.Background(), []string{"unrecognized"}, &out, &diagnostics); err == nil {
		t.Fatal("unknown command accepted")
	}
	t.Setenv("VORTEX_ENROLLMENT_TOKEN", "private-token-never-log")
	if err := runCommand(context.Background(), []string{"enroll", "--token", "private-token-never-log"}, &out, &diagnostics); err == nil {
		t.Fatal("command-line token accepted")
	}
	if strings.Contains(out.String()+diagnostics.String(), "private-token-never-log") {
		t.Fatal("token leaked")
	}
}
func TestEnrollCommandUsesLocalCSRAndRejectsSecondWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := identity.GenerateCSR("hub")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := ca.SignCSR(csr, "hub", time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	credential, err = identity.AttachKey(credential, key, der)
	if err != nil {
		t.Fatal(err)
	}
	server, err := hub.NewEnrollmentServer(ca, der, map[string]bool{"one-use-secret": true}, &tls.Config{Certificates: []tls.Certificate{credential.Certificate}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(ctx, listener) }()
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	tokenPath := filepath.Join(dir, "token")
	database := filepath.Join(dir, "agent.db")
	os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(tokenPath, []byte("one-use-secret"), 0600)
	args := []string{"enroll", "--node-id", "550e8400-e29b-41d4-a716-446655440100", "--enrollment-hub", listener.Addr().String(), "--hub-name", "hub", "--tls-ca", caPath, "--token-file", tokenPath, "--db", database, "--credential-dir", filepath.Join(dir, "credentials")}
	var out, diagnostics bytes.Buffer
	lock, err := agent.LockProcess(database)
	if err != nil {
		t.Fatal(err)
	}
	if err = runCommand(ctx, args, &out, &diagnostics); err == nil {
		t.Fatal("enroll opened competing writer")
	}
	lock()
	if err = runCommand(ctx, args, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String()+diagnostics.String(), "one-use-secret") || !strings.Contains(out.String(), "expires_at") {
		t.Fatal("unsafe enrollment output")
	}
	if err = runCommand(ctx, args, &out, &diagnostics); err == nil {
		t.Fatal("duplicate enroll replaced active credential")
	}
}

func TestUpgradePreflightReadsExistingSchemaWithoutMigrating(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.db")
	db, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	var out, diagnostics bytes.Buffer
	if err = runCommand(context.Background(), []string{"upgrade-preflight", "--db", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"read_only":true`) || !strings.Contains(out.String(), `"schema":16`) {
		t.Fatal(out.String())
	}
	missing := filepath.Join(root, "absent.db")
	if err = runCommand(context.Background(), []string{"upgrade-preflight", "--db", missing}, &out, &diagnostics); err == nil {
		t.Fatal("created missing preflight DB")
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("preflight created missing database")
	}
}
