package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrToken = errors.New("invalid or expired token")
var ErrTokenReuse = errors.New("refresh token reuse")

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	FamilyID     string `json:"-"`
}
type TokenMaterial struct {
	FamilyID                string
	AccessHash, RefreshHash []byte
	AccessTTL, RefreshTTL   time.Duration
}
type TokenStore interface {
	CreateTokens(context.Context, Principal, TokenMaterial, string) error
	RotateTokens(context.Context, []byte, TokenMaterial, string) (string, error)
	AccessPrincipal(context.Context, []byte) (Principal, error)
	RevokeTokens(context.Context, []byte, string) error
}
type Tokens struct {
	Store                 TokenStore
	AccessTTL, RefreshTTL time.Duration
}

func tokenHash(token, prefix string) ([]byte, error) {
	if !strings.HasPrefix(token, prefix) || len(token) != len(prefix)+43 {
		return nil, ErrToken
	}
	raw, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if e != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != strings.TrimPrefix(token, prefix) {
		return nil, ErrToken
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], nil
}
func newToken(prefix string) (string, []byte, error) {
	var raw [32]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return "", nil, e
	}
	v := prefix + base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(v))
	return v, sum[:], nil
}
func (m Tokens) material() (TokenPair, TokenMaterial, error) {
	var pair TokenPair
	var v TokenMaterial
	if m.Store == nil || m.AccessTTL <= 0 || m.AccessTTL > 15*time.Minute || m.RefreshTTL < m.AccessTTL || m.RefreshTTL > 30*24*time.Hour {
		return pair, v, ErrToken
	}
	var e error
	pair.AccessToken, v.AccessHash, e = newToken("va1_")
	if e != nil {
		return pair, v, e
	}
	pair.RefreshToken, v.RefreshHash, e = newToken("vr1_")
	if e != nil {
		return pair, v, e
	}
	v.FamilyID = uuid.NewString()
	v.AccessTTL = m.AccessTTL
	v.RefreshTTL = m.RefreshTTL
	pair.ExpiresIn = int64(m.AccessTTL / time.Second)
	return pair, v, nil
}

// Issue is a trusted post-authentication adapter entrypoint, never an API accepting actor/role claims.
func (m Tokens) Issue(ctx context.Context, p Principal, request string) (TokenPair, error) {
	pair, v, e := m.material()
	if e != nil {
		return TokenPair{}, e
	}
	if !p.Active() {
		return TokenPair{}, ErrToken
	}
	if e = m.Store.CreateTokens(ctx, p, v, request); e != nil {
		return TokenPair{}, e
	}
	pair.FamilyID = v.FamilyID
	return pair, nil
}
func (m Tokens) Refresh(ctx context.Context, token, request string) (TokenPair, error) {
	hash, e := tokenHash(token, "vr1_")
	if e != nil {
		return TokenPair{}, e
	}
	pair, v, e := m.material()
	if e != nil {
		return TokenPair{}, e
	}
	id, e := m.Store.RotateTokens(ctx, hash, v, request)
	if e != nil {
		return TokenPair{}, e
	}
	pair.FamilyID = id
	return pair, nil
}
func (m Tokens) Authenticate(ctx context.Context, token string) (Principal, error) {
	hash, e := tokenHash(token, "va1_")
	if e != nil || m.Store == nil {
		return Principal{}, ErrToken
	}
	return m.Store.AccessPrincipal(ctx, hash)
}
func (m Tokens) Revoke(ctx context.Context, token, request string) error {
	hash, e := tokenHash(token, "va1_")
	if e != nil || m.Store == nil {
		return ErrToken
	}
	return m.Store.RevokeTokens(ctx, hash, request)
}
