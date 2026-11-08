package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
)

type TokenRepository struct{ Pool *pgxpool.Pool }

func tokenPrincipal(ctx context.Context, q *generated.Queries, actor, tenant string) (auth.Principal, error) {
	row, e := q.TokenPrincipal(ctx, generated.TokenPrincipalParams{ID: actor, TenantID: tenant})
	if e != nil {
		return auth.Principal{}, auth.ErrToken
	}
	p, e := auth.NewPrincipal(row.ID, row.TenantID, auth.Role(row.Role), row.State)
	if e != nil || !p.Active() {
		return auth.Principal{}, auth.ErrToken
	}
	return p, nil
}
func tokenScope(p auth.Principal) (TenantScope, error) {
	tenant := p.TenantID()
	if tenant == "" {
		tenant = p.ID()
	}
	return NewTenantScope(p, tenant, policy.Read)
}
func tokenExpiry(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at.UTC(), Valid: true}
}
func appendTokenPair(ctx context.Context, q *generated.Queries, family string, v auth.TokenMaterial, limit time.Time) error {
	now := time.Now()
	access := now.Add(v.AccessTTL)
	if access.After(limit) {
		access = limit
	}
	if e := q.InsertAccessToken(ctx, generated.InsertAccessTokenParams{TokenSha256: v.AccessHash, FamilyID: family, ExpiresAt: tokenExpiry(access)}); e != nil {
		return e
	}
	return q.InsertRefreshToken(ctx, generated.InsertRefreshTokenParams{TokenSha256: v.RefreshHash, FamilyID: family, ExpiresAt: tokenExpiry(limit)})
}
func (r TokenRepository) CreateTokens(ctx context.Context, p auth.Principal, v auth.TokenMaterial, request string) error {
	persisted, e := tokenPrincipal(ctx, generated.New(r.Pool), p.ID(), p.TenantID())
	if e != nil || persisted.Role() != p.Role() {
		return auth.ErrToken
	}
	scope, e := tokenScope(persisted)
	if e != nil {
		return auth.ErrToken
	}
	event := Audit{Action: "auth.session.issue", ResourceID: v.FamilyID, RequestID: request, Reason: "trusted authentication adapter"}
	return (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &event, func(q *generated.Queries) error {
		current, e := tokenPrincipal(ctx, q, p.ID(), p.TenantID())
		if e != nil || current.Role() != persisted.Role() {
			return auth.ErrToken
		}
		kind := "tenant"
		tenant := pgtype.Text{String: p.TenantID(), Valid: true}
		if p.Platform() {
			kind = "platform"
			tenant = pgtype.Text{}
		}
		expiry := time.Now().Add(v.RefreshTTL)
		if e = q.InsertRefreshFamily(ctx, generated.InsertRefreshFamilyParams{ID: v.FamilyID, ActorID: p.ID(), SubjectKind: kind, TenantID: tenant, ExpiresAt: tokenExpiry(expiry)}); e != nil {
			return e
		}
		return appendTokenPair(ctx, q, v.FamilyID, v, expiry)
	})
}
func familyPrincipal(ctx context.Context, q *generated.Queries, f generated.RefreshTokenFamily) (auth.Principal, error) {
	return tokenPrincipal(ctx, q, f.ActorID, f.TenantID.String)
}
func (r TokenRepository) RotateTokens(ctx context.Context, hash []byte, v auth.TokenMaterial, request string) (string, error) {
	q := generated.New(r.Pool)
	f, e := q.FindRefreshFamily(ctx, hash)
	if e != nil {
		return "", auth.ErrToken
	}
	p, e := familyPrincipal(ctx, q, f)
	if e != nil {
		return "", auth.ErrToken
	}
	scope, e := tokenScope(p)
	if e != nil {
		return "", auth.ErrToken
	}
	event := Audit{Action: "auth.session.rotate", ResourceID: f.ID, RequestID: request, Reason: "single use refresh rotation"}
	reuse := false
	e = (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &event, func(q *generated.Queries) error {
		locked, e := q.LockRefreshFamily(ctx, f.ID)
		if e != nil {
			return e
		}
		if !locked.ExpiresAt.Time.After(time.Now()) {
			return auth.ErrToken
		}
		current, e := familyPrincipal(ctx, q, locked)
		if e != nil || current.Role() != p.Role() {
			return auth.ErrToken
		}
		revoked, e := q.IsRefreshFamilyRevoked(ctx, f.ID)
		if e != nil {
			return e
		}
		if revoked {
			return auth.ErrToken
		}
		token, e := q.ReadRefreshToken(ctx, generated.ReadRefreshTokenParams{TokenSha256: hash, FamilyID: f.ID})
		if e != nil {
			return auth.ErrToken
		}
		if token.ConsumedAt.Valid {
			reuse = true
			event.Action = "auth.session.reuse"
			event.Reason = "refresh reuse revokes entire family"
			return q.RevokeRefreshFamily(ctx, generated.RevokeRefreshFamilyParams{FamilyID: f.ID, Reason: "reuse"})
		}
		if !token.ExpiresAt.Time.After(time.Now()) {
			return auth.ErrToken
		}
		count, e := q.ConsumeRefreshToken(ctx, hash)
		if e != nil {
			return e
		}
		if count != 1 {
			return auth.ErrToken
		}
		return appendTokenPair(ctx, q, f.ID, v, locked.ExpiresAt.Time)
	})
	if e != nil {
		return "", e
	}
	if reuse {
		return "", auth.ErrTokenReuse
	}
	return f.ID, nil
}
func (r TokenRepository) AccessPrincipal(ctx context.Context, hash []byte) (auth.Principal, error) {
	q := generated.New(r.Pool)
	f, e := q.FindAccessFamily(ctx, hash)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return auth.Principal{}, auth.ErrToken
		}
		return auth.Principal{}, e
	}
	return familyPrincipal(ctx, q, f)
}
func (r TokenRepository) RevokeTokens(ctx context.Context, hash []byte, request string) error {
	q := generated.New(r.Pool)
	f, e := q.FindAccessFamily(ctx, hash)
	if e != nil {
		return auth.ErrToken
	}
	p, e := familyPrincipal(ctx, q, f)
	if e != nil {
		return auth.ErrToken
	}
	scope, e := tokenScope(p)
	if e != nil {
		return auth.ErrToken
	}
	return (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &Audit{Action: "auth.session.logout", ResourceID: f.ID, RequestID: request, Reason: "explicit session logout"}, func(q *generated.Queries) error {
		if _, e := q.LockRefreshFamily(ctx, f.ID); e != nil {
			return e
		}
		return q.RevokeRefreshFamily(ctx, generated.RevokeRefreshFamilyParams{FamilyID: f.ID, Reason: "logout"})
	})
}
