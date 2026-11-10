package policy_test

import (
	"github.com/google/uuid"
	"testing"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
)

func TestTenantRoleScopeMatrix(t *testing.T) {
	tenant, foreign := uuid.NewString(), uuid.NewString()
	cases := []struct {
		role    auth.Role
		actions map[policy.Action]bool
	}{
		{auth.PlatformAdmin, map[policy.Action]bool{policy.Read: true, policy.Mutate: true, policy.ManageAccess: true, policy.Restrict: true, policy.Grant: true}},
		{auth.Operator, map[policy.Action]bool{policy.Read: true, policy.Mutate: true, policy.Restrict: true}},
		{auth.Auditor, map[policy.Action]bool{policy.Read: true}},
		{auth.Owner, map[policy.Action]bool{policy.Read: true, policy.Mutate: true, policy.ManageAccess: true}},
		{auth.Member, map[policy.Action]bool{policy.Read: true, policy.Mutate: true}},
	}
	for _, c := range cases {
		t.Run(string(c.role), func(t *testing.T) {
			home := tenant
			if c.role == auth.PlatformAdmin || c.role == auth.Operator || c.role == auth.Auditor {
				home = ""
			}
			for _, state := range []string{"active", "suspended", "banned"} {
				p, err := auth.NewPrincipal(uuid.NewString(), home, c.role, state)
				if err != nil {
					t.Fatal(err)
				}
				for _, target := range []string{tenant, foreign} {
					for _, a := range []policy.Action{policy.Read, policy.Mutate, policy.ManageAccess, policy.Restrict, policy.Grant, "unknown"} {
						want := state == "active" && c.actions[a] && (home == "" || target == tenant)
						if got := policy.Authorize(p, target, a) == nil; got != want {
							t.Fatalf("state=%s target=%s action=%s got=%v want=%v", state, target, a, got, want)
						}
					}
				}
				if policy.AuthorizeGrant(p, tenant, "tenant") == nil {
					t.Fatal("self-hosted resource was assignable")
				}
			}
		})
	}
	if policy.Authorize(auth.Principal{}, tenant, policy.Read) == nil {
		t.Fatal("zero principal accepted")
	}
	for _, c := range []struct {
		tenant string
		role   auth.Role
		state  string
	}{{tenant, auth.PlatformAdmin, "active"}, {"", auth.Owner, "active"}, {tenant, "custom", "active"}, {tenant, auth.Owner, "expired"}} {
		if _, err := auth.NewPrincipal(uuid.NewString(), c.tenant, c.role, c.state); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}
