package agent

import (
	"sort"
	"vortex/engine/apply"
	ruleruntime "vortex/engine/runtime/rule"
)

type runtimeRenewalScope struct {
	RuntimeScope
	Fence uint64
}

// activeRenewalScopes observes the live process-local Engine, never a durable
// active receipt from an earlier process. Prepare is not execution evidence.
// The cursor limits one scheduler pass to32 rules and carries no authority.
func (a *RuntimeApplier) activeRenewalScopes(after string) ([]runtimeRenewalScope, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	latest := map[string]ruleruntime.Artifact{}
	for id, artifact := range a.ruleTransactions {
		tx, found := a.rules.Query(id)
		if !found || tx.State != apply.Active {
			continue
		}
		old, found := latest[artifact.RuleID]
		if !found || artifact.FencingGeneration > old.FencingGeneration {
			latest[artifact.RuleID] = artifact
		}
	}
	var scopes []runtimeRenewalScope
	for id, a := range latest {
		if id <= after || !a.Intent.Rule.Enabled {
			continue
		}
		scopes = append(scopes, runtimeRenewalScope{RuntimeScope: RuntimeScope{RuleID: id, RuleRevision: uint64(a.RuleRevision), PathGeneration: uint64(a.PathGeneration)}, Fence: uint64(a.FencingGeneration)})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].RuleID < scopes[j].RuleID })
	next := ""
	if len(scopes) > 32 {
		scopes = scopes[:32]
		next = scopes[len(scopes)-1].RuleID
	}
	return scopes, next
}
