package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

func TestRuntimeEngineStartsAndStopsThroughNode(t *testing.T) {
	dir := t.TempDir()
	const nodeID = "00000000-0000-0000-0000-000000000011"
	certPath, keyPath, caPath := writeRuntimeIdentity(t, dir, nodeID)
	listen := "127.0.0.1:0"
	configPath := filepath.Join(dir, "engine.json")
	config := `{"qualification_only":true,"evidence_file":"` + filepath.Join(dir, "events.jsonl") + `","node_id":"` + nodeID + `","role":"destination","certificate":"` + certPath + `","private_key":"` + keyPath + `","trust_bundle":"` + caPath + `","listen":"` + listen + `","version":1,"rule_id":"rule","rule_revision":1,"path_generation":1,"hop_index":0,"target":"127.0.0.1:9","execution_point":true}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := NewRuntimeEngine(configPath)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(nodeID, s, e)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	errCh := e.runtimeDone()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("Engine runtime exited during start: %v", err)
		}
	case <-time.After(50 * time.Millisecond):
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := n.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func (e *RuntimeEngine) runtimeDone() <-chan error { return e.runtime.Done() }

func writeRuntimeIdentity(t *testing.T, dir, nodeID string) (string, string, string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "vortex-test-ca"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse("urn:vortex:node:" + nodeID)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: nodeID}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{uri}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath, caPath := filepath.Join(dir, "node.pem"), filepath.Join(dir, "node-key.pem"), filepath.Join(dir, "ca.pem")
	writePEM := func(path, kind string, der []byte) {
		t.Helper()
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(certPath, "CERTIFICATE", leafDER)
	writePEM(keyPath, "PRIVATE KEY", keyDER)
	writePEM(caPath, "CERTIFICATE", caDER)
	return certPath, keyPath, caPath
}
