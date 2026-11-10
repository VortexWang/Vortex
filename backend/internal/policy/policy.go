package policy

import (
	"errors"
	"github.com/google/uuid"
	"vortex/backend/internal/auth"
)

var ErrForbidden = errors.New("forbidden tenant scope")

type Action string

const (
	Read         Action = "read"
	Mutate       Action = "mutate"
	ManageAccess Action = "manage_access"
	Restrict     Action = "restrict"
	Grant        Action = "grant"
)

func Authorize(p auth.Principal, tenant string, action Action) error {
	if !p.Active() {
		return ErrForbidden
	}
	if _, err := uuid.Parse(tenant); err != nil {
		return ErrForbidden
	}
	if !p.Platform() && p.TenantID() != tenant {
		return ErrForbidden
	}
	allowed := false
	switch p.Role() {
	case auth.PlatformAdmin:
		allowed = action == Read || action == Mutate || action == ManageAccess || action == Restrict || action == Grant
	case auth.Operator:
		allowed = action == Read || action == Mutate || action == Restrict
	case auth.Auditor:
		allowed = action == Read
	case auth.Owner:
		allowed = action == Read || action == Mutate || action == ManageAccess
	case auth.Member:
		allowed = action == Read || action == Mutate
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Management of a self-hosted resource never conveys grant authority.
func AuthorizeGrant(p auth.Principal, tenant, ownerKind string) error {
	if ownerKind != "platform" {
		return ErrForbidden
	}
	return Authorize(p, tenant, Grant)
}
