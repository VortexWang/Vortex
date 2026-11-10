package repository_test

import (
	"github.com/google/uuid"
	"testing"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

func TestRetargetScopePreservesAuthenticatedIdentityAndCannotEscalateAction(t *testing.T) {
	owner, other := uuid.NewString(), uuid.NewString()
	for _, tc := range []struct {
		name   string
		role   auth.Role
		action policy.Action
		target string
		want   bool
	}{
		{"admin_grant_cross_tenant", auth.PlatformAdmin, policy.Grant, other, true},
		{"admin_read_cannot_grant", auth.PlatformAdmin, policy.Read, other, false},
		{"operator_mutation_cannot_grant", auth.Operator, policy.Mutate, other, false},
		{"owner_mutation_cannot_grant", auth.Owner, policy.Mutate, owner, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := ""
			if tc.role == auth.Owner {
				home = owner
			}
			p, e := auth.NewPrincipal(uuid.NewString(), home, tc.role, "active")
			if e != nil {
				t.Fatal(e)
			}
			original, e := repository.NewTenantScope(p, owner, tc.action)
			if e != nil {
				t.Fatal(e)
			}
			target, e := original.ForTenant(tc.target, policy.Grant)
			if !tc.want {
				if e == nil {
					t.Fatal("action escalation")
				}
				return
			}
			if e != nil || target.ActorID() != original.ActorID() || target.TenantID() != other || !target.Platform() || !target.Allows(policy.Grant) {
				t.Fatal("principal not preserved", target, e)
			}
		})
	}
	p, _ := auth.NewPrincipal(uuid.NewString(), owner, auth.Owner, "active")
	ordinary, _ := repository.NewTenantScope(p, owner, policy.Mutate)
	if _, e := ordinary.ForTenant(other, policy.Mutate); e == nil {
		t.Fatal("ordinary user crossed tenant")
	}
	var invalid repository.TenantScope
	if _, e := invalid.ForTenant(other, policy.Grant); e == nil {
		t.Fatal("zero scope minted rights")
	}
}
