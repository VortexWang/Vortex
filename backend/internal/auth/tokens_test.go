package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/testutil"
)

func tokenFixture(t *testing.T) (*auth.Tokens, string, string) {
	t.Helper()
	pool := testutil.Postgres(t)
	tenant, user := uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants(id) VALUES($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner')`, user, tenant); err != nil {
		t.Fatal(err)
	}
	return &auth.Tokens{Store: repository.TokenRepository{Pool: pool}, AccessTTL: time.Minute, RefreshTTL: time.Hour}, tenant, user
}
func TestAuthTokenRotationReuseRevokesWholeFamilyAndStoresOnlyHashes(t *testing.T) {
	manager, tenant, user := tokenFixture(t)
	ctx := context.Background()
	p, err := auth.NewPrincipal(user, tenant, auth.Owner, "active")
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Issue(ctx, p, "request-issue")
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.Authenticate(ctx, first.AccessToken)
	if err != nil || current.ID() != user {
		t.Fatal(current, err)
	}
	next, err := manager.Refresh(ctx, first.RefreshToken, "request-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if next.RefreshToken == first.RefreshToken || next.AccessToken == first.AccessToken {
		t.Fatal("rotation reused material")
	}
	if _, err = manager.Refresh(ctx, first.RefreshToken, "request-reuse"); !errors.Is(err, auth.ErrTokenReuse) {
		t.Fatal(err)
	}
	for _, token := range []string{first.AccessToken, next.AccessToken} {
		if _, err = manager.Authenticate(ctx, token); !errors.Is(err, auth.ErrToken) {
			t.Fatal("family access survived reuse", err)
		}
	}
	if _, err = manager.Refresh(ctx, next.RefreshToken, "request-next"); !errors.Is(err, auth.ErrToken) {
		t.Fatal(err)
	}
	repo := manager.Store.(repository.TokenRepository)
	var stored []byte
	if err = repo.Pool.QueryRow(ctx, `SELECT token_sha256 FROM refresh_tokens WHERE family_id=$1 ORDER BY created_at LIMIT 1`, first.FamilyID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(first.RefreshToken))
	if hex.EncodeToString(stored) != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong stored digest")
	}
	var audit string
	if err = repo.Pool.QueryRow(ctx, `SELECT string_agg(before_summary::text||after_summary::text,'') FROM audit_events WHERE resource_id=$1`, first.FamilyID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, first.RefreshToken) || strings.Contains(audit, first.AccessToken) {
		t.Fatal("secret in audit")
	}
}
func TestAuthTokenCurrentRoleStateTTLAndSeparateBearerBoundary(t *testing.T) {
	manager, tenant, user := tokenFixture(t)
	ctx := context.Background()
	p, _ := auth.NewPrincipal(user, tenant, auth.Owner, "active")
	issued, err := manager.Issue(ctx, p, "issue")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{issued.RefreshToken, "Bearer " + issued.AccessToken, "node-mtls", strings.Repeat("a", 10000)} {
		if _, err = manager.Authenticate(ctx, token); !errors.Is(err, auth.ErrToken) {
			t.Fatal("wrong credential accepted", err)
		}
	}
	repo := manager.Store.(repository.TokenRepository)
	if _, err = repo.Pool.Exec(ctx, `UPDATE users SET role='member' WHERE id=$1`, user); err != nil {
		t.Fatal(err)
	}
	current, err := manager.Authenticate(ctx, issued.AccessToken)
	if err != nil || current.Role() != auth.Member {
		t.Fatal("stale token role", current, err)
	}
	if _, err = repo.Pool.Exec(ctx, `UPDATE tenants SET account_state='suspended' WHERE id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Authenticate(ctx, issued.AccessToken); !errors.Is(err, auth.ErrToken) {
		t.Fatal(err)
	}
	if _, err = repo.Pool.Exec(ctx, `UPDATE tenants SET account_state='active' WHERE id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Pool.Exec(ctx, `UPDATE api_access_tokens SET expires_at=now()-interval '1 second' WHERE family_id=$1`, issued.FamilyID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Authenticate(ctx, issued.AccessToken); !errors.Is(err, auth.ErrToken) {
		t.Fatal("expired access", err)
	}
	if _, err = repo.Pool.Exec(ctx, `UPDATE refresh_token_families SET expires_at=now()-interval '1 second' WHERE id=$1`, issued.FamilyID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Refresh(ctx, issued.RefreshToken, "expired"); !errors.Is(err, auth.ErrToken) {
		t.Fatal("expired refresh", err)
	}
}
func TestAuthConcurrentRefreshOneRotationAndReuseRevokesWinner(t *testing.T) {
	m, tenant, user := tokenFixture(t)
	ctx := context.Background()
	p, _ := auth.NewPrincipal(user, tenant, auth.Owner, "active")
	first, err := m.Issue(ctx, p, "issue")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins []auth.TokenPair
	reuses := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := m.Refresh(ctx, first.RefreshToken, uuid.NewString())
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				wins = append(wins, v)
			} else if errors.Is(e, auth.ErrTokenReuse) || errors.Is(e, auth.ErrToken) {
				reuses++
			} else {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(wins) != 1 || reuses != 7 {
		t.Fatal(len(wins), reuses)
	}
	if _, err = m.Authenticate(ctx, wins[0].AccessToken); !errors.Is(err, auth.ErrToken) {
		t.Fatal("winner survives reuse", err)
	}
}
func TestAuthTokenAuditFailureRollsBackRotationAndRevocation(t *testing.T) {
	m, tenant, user := tokenFixture(t)
	ctx := context.Background()
	p, _ := auth.NewPrincipal(user, tenant, auth.Owner, "active")
	first, err := m.Issue(ctx, p, "issue")
	if err != nil {
		t.Fatal(err)
	}
	repo := m.Store.(repository.TokenRepository)
	// Per-request trigger avoids affecting concurrent tests or unrelated tenants.
	name := "token_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = repo.Pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='token-audit-fault' THEN RAISE EXCEPTION 'audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`()`)
	if _, err = m.Refresh(ctx, first.RefreshToken, "token-audit-fault"); err == nil {
		t.Fatal("rotation survived audit failure")
	}
	next, err := m.Refresh(ctx, first.RefreshToken, "normal")
	if err != nil {
		t.Fatal("rollback consumed refresh", err)
	}
	if _, err = m.Refresh(ctx, first.RefreshToken, "token-audit-fault"); err == nil {
		t.Fatal("revocation survived audit failure")
	}
	if _, err = m.Authenticate(ctx, next.AccessToken); err != nil {
		t.Fatal("failed revoke changed family", err)
	}
	if err = m.Revoke(ctx, next.AccessToken, "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Authenticate(ctx, next.AccessToken); !errors.Is(err, auth.ErrToken) {
		t.Fatal("logout access", err)
	}
}

func TestAuthPlatformSubjectBindingAndIssueRejectsUnpersistedRole(t *testing.T) {
	m, tenant, user := tokenFixture(t)
	ctx := context.Background()
	repo := m.Store.(repository.TokenRepository)
	forged, _ := auth.NewPrincipal(user, tenant, auth.Member, "active")
	if _, err := m.Issue(ctx, forged, "stale-role"); !errors.Is(err, auth.ErrToken) {
		t.Fatal("unpersisted role issued", err)
	}
	platform := uuid.NewString()
	if _, err := repo.Pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, platform); err != nil {
		t.Fatal(err)
	}
	p, _ := auth.NewPrincipal(platform, "", auth.Auditor, "active")
	pair, err := m.Issue(ctx, p, "platform-issue")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := m.Authenticate(ctx, pair.AccessToken)
	if err != nil || !actual.Platform() || actual.TenantID() != "" || actual.Role() != auth.Auditor {
		t.Fatal(actual, err)
	}
	next, err := m.Refresh(ctx, pair.RefreshToken, "platform-rotate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Pool.Exec(ctx, `UPDATE platform_principals SET state='banned' WHERE id=$1`, platform); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Authenticate(ctx, next.AccessToken); !errors.Is(err, auth.ErrToken) {
		t.Fatal("banned platform access", err)
	}
	if _, err = m.Refresh(ctx, next.RefreshToken, "banned-platform"); !errors.Is(err, auth.ErrToken) {
		t.Fatal("banned platform refresh", err)
	}
}

func TestAuthTokenIssueAuditFailureAndConfigurationBounds(t *testing.T) {
	m, tenant, user := tokenFixture(t)
	ctx := context.Background()
	p, _ := auth.NewPrincipal(user, tenant, auth.Owner, "active")
	repo := m.Store.(repository.TokenRepository)
	if _, err := m.Issue(ctx, p, ""); err == nil {
		t.Fatal("missing audit context accepted")
	}
	var count int
	if err := repo.Pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_families WHERE actor_id=$1`, user).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed issue created family", count, err)
	}
	for _, ttl := range []time.Duration{-time.Second, 0, 16 * time.Minute} {
		bad := *m
		bad.AccessTTL = ttl
		if _, err := bad.Issue(ctx, p, "bad-ttl"); !errors.Is(err, auth.ErrToken) {
			t.Fatal(ttl, err)
		}
	}
	for _, ttl := range []time.Duration{time.Second, 31 * 24 * time.Hour} {
		bad := *m
		bad.RefreshTTL = ttl
		if _, err := bad.Issue(ctx, p, "bad-refresh-ttl"); !errors.Is(err, auth.ErrToken) {
			t.Fatal(ttl, err)
		}
	}
	// Abort at the audit insert, after family and both token rows were inserted.
	trigger := "auth_issue_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := repo.Pool.Exec(ctx, `CREATE FUNCTION `+trigger+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='issue-audit-failure' THEN RAISE EXCEPTION 'audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+trigger+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+trigger+`()`)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Pool.Exec(ctx, `DROP TRIGGER `+trigger+` ON audit_events; DROP FUNCTION `+trigger+`()`)
	if _, err = m.Issue(ctx, p, "issue-audit-failure"); err == nil {
		t.Fatal("issue committed without audit")
	}
	if err = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_families WHERE actor_id=$1`, user).Scan(&count); err != nil || count != 0 {
		t.Fatal("family survived audit rollback", count, err)
	}
}
