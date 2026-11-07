package control

import (
	"context"

	"encoding/json"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type CredentialStore struct {
	Pool     *pgxpool.Pool
	Crypto   *CredentialCrypto
	Provider DNSProvider
}
type CredentialRef struct {
	ID, ZoneID, ZoneName string
	KeyVersion           int64
}

func (s CredentialStore) Reconcile(ctx context.Context, scope repository.TenantScope, id, zone, name string, addresses []string, owner, request string) error {
	if s.Provider == nil {
		return ErrDNS
	}
	return s.Use(ctx, scope, id, zone, request, func(token []byte) error { return s.Provider.Reconcile(ctx, token, zone, name, addresses, owner) })
}

func currentAccess(ctx context.Context, q *generated.Queries, s repository.TenantScope, manage bool) error {
	if !s.Valid() {
		return ErrCredential
	}
	if s.Platform() {
		p, e := q.GetPlatformPrincipal(ctx, s.ActorID())
		if e != nil || p.State != "active" || (manage && p.Role != "platform_admin") || (!manage && p.Role != "platform_admin" && p.Role != "operator") {
			return ErrCredential
		}
		return nil
	}
	p, e := q.GetTenantUser(ctx, generated.GetTenantUserParams{TenantID: s.TenantID(), ID: s.ActorID()})
	if e != nil || p.State != "active" || (manage && p.Role != "owner") || (!manage && p.Role != "owner" && p.Role != "member") {
		return ErrCredential
	}
	return nil
}

func (s CredentialStore) Store(ctx context.Context, scope repository.TenantScope, id, zone string, token []byte, request string) (CredentialRef, error) {
	if !scope.Allows(policy.ManageAccess) || uuid.Validate(id) != nil || !cfID.MatchString(zone) || !validToken(token) || s.Provider == nil {
		return CredentialRef{}, ErrCredential
	}
	// Validate external scope before the short database transaction. An invalid
	// token never reaches persistence, task payloads, audit summaries or errors.
	verified, e := s.Provider.VerifyZone(ctx, token, zone)
	if e != nil || verified.ID != zone || !dnsName.MatchString(verified.Name) {
		return CredentialRef{}, ErrCredential
	}
	ciphertext, nonce, version, e := s.Crypto.seal(scope.TenantID(), id, zone, token)
	if e != nil {
		return CredentialRef{}, e
	}
	ref := CredentialRef{ID: id, ZoneID: zone, ZoneName: verified.Name, KeyVersion: version}
	summary, _ := json.Marshal(ref)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.ManageAccess, &repository.Audit{Action: "dns.access.store", ResourceID: id, RequestID: request, Reason: "validated dedicated zone; encrypted at rest", After: summary}, func(q *generated.Queries) error {
		if e := currentAccess(ctx, q, scope, true); e != nil {
			return e
		}
		if _, e := q.InsertEncryptedCredential(ctx, generated.InsertEncryptedCredentialParams{ID: id, TenantID: scope.TenantID(), ZoneID: zone, Ciphertext: ciphertext, Nonce: nonce, KeyVersion: version, Scope: []string{"zone:" + zone + ":dns:write"}, ZoneName: verified.Name}); e != nil {
			return e
		}
		_, e := repository.EnqueueDNS(ctx, q, scope, "access.stored/"+request, struct {
			Action string `json:"action"`
			ID     string `json:"access_id"`
		}{"dns.access.stored", id})
		return e
	})
	if e != nil {
		return CredentialRef{}, e
	}
	return ref, nil
}

func (s CredentialStore) Use(ctx context.Context, scope repository.TenantScope, id, zone, request string, fn func([]byte) error) error {
	var row generated.EncryptedCredential
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "dns.access.use", ResourceID: id, RequestID: request, Reason: "scoped provider use; no plaintext returned to transport"}, func(q *generated.Queries) error {
		if e := currentAccess(ctx, q, scope, false); e != nil {
			return e
		}
		var e error
		row, e = q.GetEncryptedCredential(ctx, generated.GetEncryptedCredentialParams{TenantID: scope.TenantID(), ID: id})
		return e
	})
	if e != nil {
		return ErrCredential
	}
	if row.ZoneID != zone || len(row.Scope) != 1 || row.Scope[0] != "zone:"+zone+":dns:write" {
		return ErrCredential
	}
	token, e := s.Crypto.open(scope.TenantID(), id, zone, row.KeyVersion, row.Ciphertext, row.Nonce)
	if e != nil {
		return e
	}
	defer clear(token)
	return fn(token)
}

func (s CredentialStore) Delete(ctx context.Context, scope repository.TenantScope, id, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.ManageAccess, &repository.Audit{Action: "dns.access.delete", ResourceID: id, RequestID: request, Reason: "remove encrypted provider access; no port release"}, func(q *generated.Queries) error {
		if e := currentAccess(ctx, q, scope, true); e != nil {
			return e
		}
		count, e := q.DeleteEncryptedCredential(ctx, generated.DeleteEncryptedCredentialParams{TenantID: scope.TenantID(), ID: id})
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrCredential
		}
		_, e = repository.EnqueueDNS(ctx, q, scope, "access.deleted/"+request, struct {
			Action string `json:"action"`
			ID     string `json:"access_id"`
		}{"dns.access.deleted", id})
		return e
	})
}
