package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

// effectiveRuntimeEpochTx preserves the prepared artifact's original epoch and
// derives a later live signer only through both exact original aggregate shares'
// complete admitted S/R chains. It never adopts a cold generation or changes an
// artifact. Candidate reads are advisory; funding repeats this under Hub/tenant
// locks before issuing any authority or reserving source funds.
func effectiveRuntimeEpochTx(ctx context.Context, q *generated.Queries, tenant, node, boot string, artifactEpoch, path, fence int64) (int64, error) {
	hub, e := q.UsageNodeHub(ctx, node)
	if e != nil {
		return 0, runtimeEpochError(e)
	}
	current, e := q.LatestHubIncarnation(ctx, hub)
	if e != nil {
		return 0, runtimeEpochError(e)
	}
	if current.SignerEpoch == artifactEpoch {
		return artifactEpoch, nil
	}
	if current.SignerEpoch < artifactEpoch {
		return 0, ErrShareUnknown
	}
	for _, kind := range []string{"rate", "connections"} {
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: tenant, NodeID: node, Kind: kind})
		if e != nil {
			return 0, runtimeEpochError(e)
		}
		if chain.BootID != boot || chain.ActiveGeneration < 1 || chain.PendingGeneration != 0 {
			return 0, ErrShareUnknown
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: tenant, NodeID: node, Kind: kind, Generation: chain.ActiveGeneration})
		if e != nil {
			return 0, runtimeEpochError(e)
		}
		if g.BootID != boot || g.SignerID != hub || g.SignerEpoch != artifactEpoch || g.PathGeneration != path || g.FencingGeneration > fence {
			return 0, ErrShareUnknown
		}
		a, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: tenant, NodeID: node, Kind: kind, Generation: g.Generation, SignerID: hub})
		if e != nil {
			return 0, runtimeEpochError(e)
		}
		issuer, e := effectiveShareIssuerTx(ctx, q, g, a)
		if e != nil {
			return 0, runtimeEpochError(e)
		}
		if !issuer.Recovered || issuer.Epoch != current.SignerEpoch || issuer.Boot != current.BootID {
			return 0, ErrShareUnknown
		}
	}
	return current.SignerEpoch, nil
}

// Missing authority is a pending proof, never a new signing permission.
func runtimeEpochError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrShareUnknown
	}
	return e
}
