//go:build cloudflareintegration

package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
	"vortex/backend/db/generated"
)

// This test is deliberately opt-in through the script, never skipped or replaced
// by an HTTP fixture. Only api.cloudflare.com can receive this token.
func TestCloudflareControlledCRUD(t *testing.T) {
	token := []byte(os.Getenv("CLOUDFLARE_API_TOKEN"))
	defer clear(token)
	zone := os.Getenv("CLOUDFLARE_ZONE_ID")
	if !validToken(token) || !cfID.MatchString(zone) {
		t.Fatal("dedicated Cloudflare token and zone required; real CRUD not executed")
	}
	policyPath := os.Getenv("CLOUDFLARE_TOKEN_POLICY_FILE")
	var provider *Cloudflare
	if policyPath != "" {
		info, e := os.Lstat(policyPath)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
			t.Fatal("policy file must be a private regular file, at most 64 KiB")
		}
		raw, e := os.ReadFile(policyPath)
		if e != nil {
			t.Fatal("cannot read reviewed policy evidence")
		}
		provider = NewCloudflareWithPolicyEvidence(raw)
	} else {
		if !validToken([]byte(os.Getenv("CLOUDFLARE_POLICY_TOKEN"))) {
			t.Fatal("reviewed token policy file or independent read-only policy inspector required")
		}
		provider = NewCloudflare([]byte(os.Getenv("CLOUDFLARE_POLICY_TOKEN")))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	verified, e := provider.VerifyZone(ctx, token, zone)
	if e != nil {
		t.Fatal("token activity, identity-bound exact-zone policy or dedicated zone verification failed")
	}
	store, scope := credentialFixture(t)
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		t.Fatal("test encryption key generation failed")
	}
	defer clear(key)
	store.Crypto, e = NewCredentialCrypto(1, map[int64][]byte{1: key})
	if e != nil {
		t.Fatal("key setup failed")
	}
	store.Provider = provider
	access := uuid.NewString()
	if _, e = store.Store(ctx, scope, access, zone, token, uuid.NewString()); e != nil {
		t.Fatal("encrypted live access enrollment failed")
	}
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if e := store.Delete(cleanupCtx, scope, access, uuid.NewString()); e != nil {
			t.Error("live encrypted access cleanup failed")
		}
	}()
	row, e := generated.New(store.Pool).GetEncryptedCredential(ctx, generated.GetEncryptedCredentialParams{TenantID: scope.TenantID(), ID: access})
	if e != nil || bytes.Contains(row.Ciphertext, token) || len(row.Nonce) != 12 || row.KeyVersion != 1 {
		t.Fatal("encrypted storage assertion failed")
	}
	name := "vortex-phase6-" + uuid.NewString() + "." + verified.Name
	owner := scope.TenantID() + ":" + uuid.NewString()
	mutation := credentialMutation(t, scope)
	// Always reconcile the exact unique name after an ambiguous POST, including
	// when the provider did not return its ID. Never delete another owner/name.
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()
		e := store.Use(cleanupCtx, mutation, access, zone, uuid.NewString(), func(token []byte) error { return provider.Reconcile(cleanupCtx, token, zone, name, nil, owner) })
		if e != nil {
			t.Errorf("controlled DNS cleanup failed; dedicated temporary name=%s; manual review required", name)
		}
	}()
	use := func(fn func([]byte) error) {
		t.Helper()
		if e := store.Use(ctx, mutation, access, zone, uuid.NewString(), fn); e != nil {
			t.Fatal("controlled provider step failed; cleanup requested")
		}
	}
	use(func(token []byte) error {
		rows, e := provider.List(ctx, token, zone, name)
		if e != nil || len(rows) != 0 {
			return ErrDNS
		}
		return nil
	})
	var record DNSRecord
	use(func(token []byte) error {
		var e error
		record, e = provider.Create(ctx, token, zone, DNSRecord{Type: "A", Name: name, Content: "192.0.2.1", TTL: 60, Comment: "vortex:" + owner})
		return e
	})
	use(func(token []byte) error {
		actual, e := provider.Read(ctx, token, zone, record.ID)
		if e != nil || !recordMatches(actual, record) {
			return ErrDNS
		}
		return nil
	})
	record.Content = "192.0.2.2"
	use(func(token []byte) error {
		actual, e := provider.Update(ctx, token, zone, record)
		if e != nil || !recordMatches(actual, record) {
			return ErrDNS
		}
		return nil
	})
	use(func(token []byte) error { return provider.Delete(ctx, token, zone, record.ID) })
	use(func(token []byte) error {
		rows, e := provider.List(ctx, token, zone, name)
		if e != nil || len(rows) != 0 {
			return ErrDNS
		}
		return nil
	})
	// A deliberately wrong zone is rejected from reviewed policy before any DNS
	// request. A missing token must fail before network I/O as well.
	wrong := strings.Repeat("f", 32)
	if zone == wrong {
		wrong = strings.Repeat("e", 32)
	}
	if _, e = provider.VerifyZone(ctx, token, wrong); !errors.Is(e, ErrCredential) {
		t.Fatal("zone mismatch accepted")
	}
	if _, e = provider.VerifyZone(ctx, nil, zone); !errors.Is(e, ErrCredential) {
		t.Fatal("missing token accepted")
	}
	t.Log("real Cloudflare create/read/update/delete passed; TTL=60; proxied=false; policy/token identity bound; encrypted storage and exact-name cleanup verified")
}
