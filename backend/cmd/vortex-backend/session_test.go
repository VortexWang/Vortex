package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/testutil"
)

func TestLocalSessionUsesPersistedPrincipalAndSeparateCredentialNamespace(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, e := pool.Exec(ctx, `INSERT INTO tenants(id) VALUES($1)`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'member')`, actor, tenant); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := issueSession(ctx, pool, []string{"--actor", actor, "--tenant", tenant}, &out); e != nil {
		t.Fatal(e)
	}
	var pair auth.TokenPair
	if e := json.Unmarshal(out.Bytes(), &pair); e != nil {
		t.Fatal(e)
	}
	tokens := auth.Tokens{Store: repository.TokenRepository{Pool: pool}}
	p, e := tokens.Authenticate(ctx, pair.AccessToken)
	if e != nil || p.ID() != actor || p.Role() != auth.Member || p.Platform() || pair.ExpiresIn != 300 {
		t.Fatal("persisted role was not used", p, e)
	}
	if _, e = tokens.Authenticate(ctx, pair.RefreshToken); e == nil {
		t.Fatal("refresh became access")
	}
	if _, e := pool.Exec(ctx, `UPDATE users SET state='suspended' WHERE id=$1`, actor); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := issueSession(ctx, pool, []string{"--actor", actor, "--tenant", tenant}, &out); e == nil || out.Len() != 0 {
		t.Fatal("suspended local actor received credentials", e)
	}
	if _, e := pool.Exec(ctx, `UPDATE users SET state='active' WHERE id=$1`, actor); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"--actor", actor}, {"--actor", actor, "--tenant", uuid.NewString()}, {"--actor", actor, "--tenant", tenant, "--role", "platform_admin"}, {"--actor", "not-uuid", "--tenant", tenant}, {"--actor", actor, "--tenant", tenant, "extra"}} {
		out.Reset()
		if e := issueSession(ctx, pool, args, &out); e == nil || out.Len() != 0 {
			t.Fatal("untrusted role/scope/arguments accepted", args, e)
		}
	}
}
func TestLocalPlatformSessionPersistsIssueAudit(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	actor := uuid.NewString()
	if _, e := pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := issueSession(ctx, pool, []string{"--actor", actor}, &out); e != nil {
		t.Fatal(e)
	}
	var pair auth.TokenPair
	json.Unmarshal(out.Bytes(), &pair)
	p, e := (auth.Tokens{Store: repository.TokenRepository{Pool: pool}}).Authenticate(ctx, pair.AccessToken)
	if e != nil || p.Role() != auth.Auditor || !p.Platform() {
		t.Fatal(p, e)
	}
	var audit int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='auth.session.issue'`, actor).Scan(&audit); e != nil || audit != 1 {
		t.Fatal(audit, e)
	}
}

func TestLocalSessionAuditFailureOutputsNoCredential(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	actor := uuid.NewString()
	if _, e := pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	name := "local_session_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+actor+`' AND NEW.action='auth.session.issue' THEN RAISE EXCEPTION 'injected issue audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	var out bytes.Buffer
	if e := issueSession(ctx, pool, []string{"--actor", actor}, &out); e == nil || out.Len() != 0 {
		t.Fatal("credential output escaped failed audit", e)
	}
	var families int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_families WHERE actor_id=$1`, actor).Scan(&families); e != nil || families != 0 {
		t.Fatal("family escaped rollback", families, e)
	}
}
