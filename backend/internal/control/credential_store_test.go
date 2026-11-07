package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/testutil"
)

type zoneProvider struct{ reject bool }

func (p zoneProvider) VerifyZone(_ context.Context, _ []byte, zone string) (DNSZone, error) {
	if p.reject {
		return DNSZone{}, ErrCredential
	}
	return DNSZone{ID: zone, Name: "example.test"}, nil
}
func (p zoneProvider) Reconcile(context.Context, []byte, string, string, []string, string) error {
	return nil
}
func credentialFixture(t *testing.T) (CredentialStore, repository.TenantScope) {
	t.Helper()
	pool := testutil.Postgres(t)
	ctx := context.Background()
	q := generated.New(pool)
	tenant := uuid.NewString()
	if _, e := q.InsertTenant(ctx, tenant); e != nil {
		t.Fatal(e)
	}
	actor := uuid.NewString()
	if e := q.InsertTenantUser(ctx, generated.InsertTenantUserParams{ID: actor, TenantID: tenant, Role: "owner", State: "active"}); e != nil {
		t.Fatal(e)
	}
	p, e := auth.NewPrincipal(actor, tenant, auth.Owner, "active")
	if e != nil {
		t.Fatal(e)
	}
	scope, e := repository.NewTenantScope(p, tenant, policy.ManageAccess)
	if e != nil {
		t.Fatal(e)
	}
	crypto, e := NewCredentialCrypto(1, map[int64][]byte{1: bytes.Repeat([]byte{7}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	return CredentialStore{Pool: pool, Crypto: crypto, Provider: zoneProvider{}}, scope
}
func credentialMutation(t *testing.T, s repository.TenantScope) repository.TenantScope {
	t.Helper()
	p, e := auth.NewPrincipal(s.ActorID(), s.TenantID(), auth.Owner, "active")
	if e != nil {
		t.Fatal(e)
	}
	scope, e := repository.NewTenantScope(p, s.TenantID(), policy.Mutate)
	if e != nil {
		t.Fatal(e)
	}
	return scope
}
func TestCredentialCryptoScopeAndRecovery(t *testing.T) {
	c, e := NewCredentialCrypto(2, map[int64][]byte{1: bytes.Repeat([]byte{1}, 32), 2: bytes.Repeat([]byte{2}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	ciphertext, nonce, version, e := c.seal("tenant", "id", "zone", []byte("secret token"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte("secret token")) || version != 2 || len(nonce) != 12 {
		t.Fatal("plaintext/metadata")
	}
	for _, args := range [][3]string{{"other", "id", "zone"}, {"tenant", "other", "zone"}, {"tenant", "id", "other"}} {
		if _, e = c.open(args[0], args[1], args[2], version, ciphertext, nonce); e == nil {
			t.Fatal("cross-scope ciphertext substitution accepted")
		}
	}
	damaged := append([]byte(nil), ciphertext...)
	damaged[0] ^= 1
	if _, e = c.open("tenant", "id", "zone", version, damaged, nonce); e == nil {
		t.Fatal("tamper accepted")
	}
	if _, e = c.open("tenant", "id", "zone", 1, ciphertext, nonce); e == nil {
		t.Fatal("key version substitution accepted")
	}
	recovered, _ := NewCredentialCrypto(3, map[int64][]byte{2: bytes.Repeat([]byte{2}, 32), 3: bytes.Repeat([]byte{3}, 32)})
	plaintext, e := recovered.open("tenant", "id", "zone", version, ciphertext, nonce)
	if e != nil || string(plaintext) != "secret token" {
		t.Fatal("retained key version recovery", e)
	}
	lost, _ := NewCredentialCrypto(3, map[int64][]byte{3: bytes.Repeat([]byte{3}, 32)})
	if _, e = lost.open("tenant", "id", "zone", version, ciphertext, nonce); !errors.Is(e, ErrCredential) {
		t.Fatal("missing key did not fail closed", e)
	}
}

func TestCredentialEncryptedScopedAuditAndRollback(t *testing.T) {
	store, scope := credentialFixture(t)
	ctx := context.Background()
	zone := strings.Repeat("a", 32)
	id := uuid.NewString()
	token := []byte("dedicated-test-token-never-log")
	ref, e := store.Store(ctx, scope, id, zone, token, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if ref.KeyVersion != 1 {
		t.Fatal(ref)
	}
	row, e := generated.New(store.Pool).GetEncryptedCredential(ctx, generated.GetEncryptedCredentialParams{TenantID: scope.TenantID(), ID: id})
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(row.Ciphertext, token) || len(row.Nonce) != 12 {
		t.Fatal("plaintext credential persisted")
	}
	var body string
	if e = store.Pool.QueryRow(ctx, "SELECT string_agg(payload::text,' ') FROM operations WHERE tenant_id=$1", scope.TenantID()).Scan(&body); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(body, string(token)) {
		t.Fatal("secret in task")
	}
	mutation := credentialMutation(t, scope)
	used := false
	if e = store.Use(ctx, mutation, id, zone, uuid.NewString(), func(actual []byte) error { used = bytes.Equal(actual, token); return nil }); e != nil || !used {
		t.Fatal("decryption use", e)
	}
	if e = store.Use(ctx, mutation, id, strings.Repeat("b", 32), uuid.NewString(), func([]byte) error { t.Fatal("zone mismatch used token"); return nil }); e == nil {
		t.Fatal("zone substitution")
	}
	_, foreign := credentialFixture(t)
	if e = store.Use(ctx, credentialMutation(t, foreign), id, zone, uuid.NewString(), func([]byte) error { t.Fatal("cross tenant token use"); return nil }); e == nil {
		t.Fatal("cross tenant lookup")
	}
	// The real database rejects the audit append after all mutation rows exist.
	// Everything, including ciphertext, task and outbox, must roll back.
	blockedRequest := "block-" + uuid.NewString()
	trigger := credentialAuditFailure(t, store.Pool, blockedRequest)
	defer trigger()
	failedID := uuid.NewString()
	if _, e = store.Store(ctx, scope, failedID, strings.Repeat("b", 32), token, blockedRequest); e == nil {
		t.Fatal("audit failure accepted")
	}
	for _, table := range []string{"encrypted_credentials", "operations", "tasks", "outbox"} {
		var count int
		query := "SELECT count(*) FROM " + table + " WHERE tenant_id=$1"
		if e = store.Pool.QueryRow(ctx, query, scope.TenantID()).Scan(&count); e != nil || count != 1 {
			t.Fatal("partial mutation persisted", table, count, e)
		}
	}
	rejected := store
	rejected.Provider = zoneProvider{reject: true}
	if _, e = rejected.Store(ctx, scope, uuid.NewString(), strings.Repeat("c", 32), token, uuid.NewString()); e == nil {
		t.Fatal("unverified zone persisted")
	}
	if _, e = store.Pool.Exec(ctx, "UPDATE users SET state='suspended' WHERE tenant_id=$1 AND id=$2", scope.TenantID(), scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if e = store.Use(ctx, mutation, id, zone, uuid.NewString(), func([]byte) error { t.Fatal("stale principal used token"); return nil }); e == nil {
		t.Fatal("persisted role/state not checked")
	}
}
func credentialAuditFailure(t *testing.T, pool *pgxpool.Pool, request string) func() {
	t.Helper()
	name := "dns_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	// Both identifiers and request are generated locally and contain only hex/dashes.
	sql := "CREATE FUNCTION " + name + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='" + request + "' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$"
	if _, e := pool.Exec(context.Background(), sql); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(context.Background(), "CREATE TRIGGER "+name+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+name+"()"); e != nil {
		t.Fatal(e)
	}
	return func() {
		_, e := pool.Exec(context.Background(), "DROP TRIGGER "+name+" ON audit_events; DROP FUNCTION "+name+"()")
		if e != nil {
			t.Error(e)
		}
	}
}

func TestCredentialErrorsRedactProviderSecrets(t *testing.T) {
	b, e := json.Marshal(CredentialRef{ID: "id", ZoneID: "zone", KeyVersion: 1})
	if e != nil || strings.Contains(string(b), "token") {
		t.Fatal("unsafe reference")
	}
	if strings.Contains(ErrCredential.Error(), "dedicated-test-token") {
		t.Fatal("secret in error")
	}
}
