-- name: TokenPrincipal :one
SELECT u.id,u.tenant_id,u.role,(CASE WHEN t.account_state='active' THEN u.state ELSE t.account_state END)::text AS state
FROM users u JOIN tenants t ON t.id=u.tenant_id WHERE u.id=$1 AND u.tenant_id=$2
UNION ALL SELECT p.id,'' AS tenant_id,p.role,p.state FROM platform_principals p WHERE p.id=$1 AND $2='';

-- name: InsertRefreshFamily :exec
INSERT INTO refresh_token_families(id,actor_id,subject_kind,tenant_id,expires_at) VALUES($1,$2,$3,$4,$5);

-- name: InsertRefreshToken :exec
INSERT INTO refresh_tokens(token_sha256,family_id,expires_at) VALUES($1,$2,$3);

-- name: InsertAccessToken :exec
INSERT INTO api_access_tokens(token_sha256,family_id,expires_at) VALUES($1,$2,$3);

-- name: FindRefreshFamily :one
SELECT f.* FROM refresh_token_families f JOIN refresh_tokens t ON t.family_id=f.id WHERE t.token_sha256=$1;

-- name: LockRefreshFamily :one
SELECT f.* FROM refresh_token_families f WHERE f.id=$1 FOR UPDATE;

-- name: ReadRefreshToken :one
SELECT * FROM refresh_tokens WHERE token_sha256=$1 AND family_id=$2;

-- name: ConsumeRefreshToken :execrows
UPDATE refresh_tokens SET consumed_at=now() WHERE token_sha256=$1 AND consumed_at IS NULL;

-- name: RevokeRefreshFamily :exec
INSERT INTO refresh_token_revocations(family_id,reason) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: IsRefreshFamilyRevoked :one
SELECT EXISTS(SELECT 1 FROM refresh_token_revocations WHERE family_id=$1);

-- name: FindAccessFamily :one
SELECT f.* FROM refresh_token_families f JOIN api_access_tokens a ON a.family_id=f.id
WHERE a.token_sha256=$1 AND a.expires_at>now() AND f.expires_at>now()
AND NOT EXISTS(SELECT 1 FROM refresh_token_revocations r WHERE r.family_id=f.id);
