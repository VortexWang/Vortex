package agent

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// CloseIssuedCreditGrant is the local consumer for an authenticated backend's
// original funded Hub issue. Transport must independently authenticate and pin
// that backend. Inspecting these bytes grants no new authority and never updates
// clocks, source roots, epochs, runtime leases or shares. The current Node boot
// must match, even if no authorization was installed in this local database.
func (n *Node) CloseIssuedCreditGrant(ctx context.Context, hub string, original *controlv1.AuthorizationUpdate, grant string) (store.CreditGrantFinal, error) {
	var zero store.CreditGrantFinal
	if n == nil || n.Store == nil || !canonicalUUID(hub) || !canonicalUUID(grant) || original == nil || len(original.ProtoReflect().GetUnknown()) != 0 {
		return zero, store.ErrAuthorizationDenied
	}
	p, err := validateAuthorizationUpdate(original.Payload, n.NodeID, n.BootID, original.Generation)
	if err != nil {
		return zero, err
	}
	if p.SchemaVersion != "1.2.0" {
		return zero, store.ErrAuthorizationDenied
	}
	for _, reference := range p.ShareReferences {
		if reference.IssuerID != hub {
			return zero, store.ErrAuthorizationDenied
		}
	}
	var expected store.AuthorizationRecord
	for _, a := range p.Authorizations {
		if a.AuthorizationID == grant && a.Kind == "credit_grant" {
			expected = authorizationStoreRecord(a, p, hub)
		}
	}
	if expected.AuthorizationID == "" || expected.DelegationID == "" || expected.ShareReferenceID == "" {
		return zero, store.ErrAuthorizationDenied
	}
	if err := n.Store.StopMatchingCreditGrant(ctx, n.NodeID, n.BootID, expected); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return n.Store.StopUninstalledCreditGrant(ctx, n.NodeID, n.BootID, expected)
		}
		return zero, err
	}
	return n.Store.FinalizeStoppedCreditGrant(ctx, n.NodeID, n.BootID, hub, grant)
}

func authorizationStoreRecord(a authorizationRecord, p authorizationPayload, issuer string) store.AuthorizationRecord {
	r := store.AuthorizationRecord{
		IssuerID: issuer, ShareReferenceID: a.ShareReferenceID, DelegationID: a.DelegationID,
		AuthorizationID: a.AuthorizationID, ParentAuthorizationID: a.ParentAuthorizationID,
		SourceID: a.SourceID, Kind: a.Kind, RuleID: a.RuleID,
		RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration,
		SignerEpoch: p.SignerEpoch, BootID: p.BootID,
		IssuedAt: a.IssuedAt.UTC().Format(time.RFC3339Nano), NotBefore: a.NotBefore.UTC().Format(time.RFC3339Nano),
		ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339Nano), HardDeadline: a.HardDeadline.UTC().Format(time.RFC3339Nano),
		BillingSnapshotID: a.BillingSnapshotID,
	}
	if a.AmountBytes != nil {
		r.AmountBytes = *a.AmountBytes
	}
	if a.RateBPS != nil {
		r.RateBPS = *a.RateBPS
	}
	if a.Connections != nil {
		r.Connections = *a.Connections
	}
	return r
}
