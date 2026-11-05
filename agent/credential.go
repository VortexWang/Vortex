package agent

import (
	"context"
	"fmt"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
)

func PersistCredential(ctx context.Context, db *store.Store, credential identity.Credential) error {
	if db == nil || credential.Leaf == nil || credential.NodeID == "" || credential.Leaf.Subject.CommonName != credential.NodeID {
		return fmt.Errorf("agent: invalid runtime credential")
	}
	if len(credential.Leaf.DNSNames) != 1 || credential.Leaf.DNSNames[0] != credential.NodeID {
		return fmt.Errorf("agent: runtime credential node SAN mismatch")
	}
	return db.SaveCredentialRotation(ctx, credential.NodeID, credential.Leaf.Raw, credential.Leaf.NotAfter, nil, time.Time{}, time.Time{})
}
