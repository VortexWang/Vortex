// Package auth carries server-authenticated identities, never request-selected roles.
package auth

import (
	"errors"
	"github.com/google/uuid"
)

type Role string

const (
	PlatformAdmin Role = "platform_admin"
	Operator      Role = "operator"
	Auditor       Role = "auditor"
	Owner         Role = "owner"
	Member        Role = "member"
)

var ErrPrincipal = errors.New("invalid principal")

type Principal struct {
	id, tenant, state string
	role              Role
}

// NewPrincipal is called by a trusted authentication adapter after reading the
// persisted role and state. Transport handlers must never accept these as claims
// from an unsigned body. Ordinary users have exactly one tenant.
func NewPrincipal(id, tenant string, role Role, state string) (Principal, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Principal{}, ErrPrincipal
	}
	if state != "active" && state != "suspended" && state != "banned" {
		return Principal{}, ErrPrincipal
	}
	switch role {
	case Owner, Member:
		if _, err := uuid.Parse(tenant); err != nil {
			return Principal{}, ErrPrincipal
		}
	case PlatformAdmin, Operator, Auditor:
		if tenant != "" {
			return Principal{}, ErrPrincipal
		}
	default:
		return Principal{}, ErrPrincipal
	}
	return Principal{id: id, tenant: tenant, role: role, state: state}, nil
}
func (p Principal) ID() string       { return p.id }
func (p Principal) TenantID() string { return p.tenant }
func (p Principal) Role() Role       { return p.role }
func (p Principal) Active() bool     { return p.id != "" && p.state == "active" }
func (p Principal) Platform() bool {
	return p.tenant == "" && (p.role == PlatformAdmin || p.role == Operator || p.role == Auditor)
}
