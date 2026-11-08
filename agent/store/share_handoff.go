package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/bits"
	"time"
	"vortex/contracts"
)

// ApplyShareHandoff is called only by the authenticated backend control
// adapter. FULL SQLite commit atomically raises the minimum generation and
// changes the capacity read by every real runtime OPEN/reservation. No in-memory
// Engine mirror can acknowledge a capacity decision ahead of this transaction.
func (s *Store) ApplyShareHandoff(ctx context.Context, node, boot, backend, phase string, raw []byte, now IssuerTime) (contracts.ShareObservation, error) {
	var out contracts.ShareObservation
	c, e := contracts.DecodeShareHandoff(raw)
	if e != nil {
		return contracts.ShareObservation{}, e
	}
	if s.owner != "agent" || c.NodeID != node || c.BootID != boot || !validIssuerUUID(backend) || (phase != "cas" && phase != "query") {
		return contracts.ShareObservation{}, ErrAuthorizationDenied
	}
	digest := contracts.SharePayloadDigest(raw)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return contracts.ShareObservation{}, e
	}
	defer tx.Rollback()
	var old, payload []byte
	var owner string
	e = tx.QueryRowContext(ctx, `SELECT payload,backend_id,receipt FROM node_share_commands WHERE handoff_id=?`, c.ID).Scan(&payload, &owner, &old)
	found := e == nil
	if found {
		if !bytes.Equal(payload, raw) || owner != backend {
			return contracts.ShareObservation{}, ErrAuthorizationUpdateConflict
		}
		if phase == "cas" {
			if json.Unmarshal(old, &out) != nil {
				return contracts.ShareObservation{}, ErrAuthorizationDenied
			}
			if e = tx.Commit(); e != nil {
				return contracts.ShareObservation{}, e
			}
			return out, nil
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return contracts.ShareObservation{}, e
	}
	var generation, floor int64
	var oldBoot string
	e = tx.QueryRowContext(ctx, `SELECT boot_id,generation,fencing_generation FROM node_share_heads WHERE tenant_id=? AND node_id=? AND kind=?`, c.TenantID, node, c.Kind).Scan(&oldBoot, &generation, &floor)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return contracts.ShareObservation{}, e
	}
	if phase == "cas" {
		if e = checkIssuerTime(now); e != nil {
			return contracts.ShareObservation{}, e
		}
		if now.Earliest.Before(c.IssuedAt) || !now.Latest.Before(c.OwnershipUntil) || !now.Latest.Before(c.PolicyUntil) || !now.Latest.Before(c.AutonomyUntil) {
			return contracts.ShareObservation{}, ErrAuthorityExpired
		}
		if generation != c.Predecessor || c.FencingGeneration < floor || oldBoot != "" && oldBoot != boot {
			return contracts.ShareObservation{}, ErrAuthorizationGenerationStale
		}
		var tenant string
		e = tx.QueryRowContext(ctx, `SELECT tenant_id FROM node_share_bindings WHERE node_id=? AND rule_id=?`, node, c.RuleID).Scan(&tenant)
		if e == nil && tenant != c.TenantID {
			return contracts.ShareObservation{}, ErrAuthorizationDenied
		}
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return contracts.ShareObservation{}, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO node_share_bindings(node_id,rule_id,tenant_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`, node, c.RuleID, c.TenantID); e != nil {
			return contracts.ShareObservation{}, e
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO node_share_heads(tenant_id,node_id,kind,boot_id,generation,fencing_generation,amount,burst_bytes,payload) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,node_id,kind) DO UPDATE SET generation=excluded.generation,fencing_generation=excluded.fencing_generation,amount=excluded.amount,burst_bytes=excluded.burst_bytes,payload=excluded.payload WHERE node_share_heads.generation=? AND node_share_heads.boot_id=excluded.boot_id`, c.TenantID, node, c.Kind, boot, c.Generation, c.FencingGeneration, c.Amount, c.BurstBytes, raw, c.Predecessor)
		if e != nil {
			return contracts.ShareObservation{}, e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return contracts.ShareObservation{}, ErrAuthorizationGenerationStale
		}
		generation = c.Generation
		floor = c.FencingGeneration
		oldBoot = boot
	}
	out = contracts.ShareObservation{HandoffID: c.ID, NodeID: node, BootID: boot, Kind: c.Kind, CommandDigest: digest, Generation: c.Generation, MinimumGeneration: generation, ActiveGeneration: generation, FencingGeneration: floor, State: "unknown"}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return contracts.ShareObservation{}, e
	}
	out.ReceiptID = hex.EncodeToString(id[:])
	if generation == c.Generation && oldBoot == boot && found || phase == "cas" {
		out.State = "enforced"
	}
	if out.OpenConnections, e = countNodeShareConnectionsTx(ctx, tx, c.TenantID, node); e != nil {
		return contracts.ShareObservation{}, e
	}
	if c.Kind == "connections" && out.State == "enforced" && out.OpenConnections > c.Amount {
		out.State = "draining"
	}
	if phase == "cas" {
		encoded, e := json.Marshal(out)
		if e != nil {
			return contracts.ShareObservation{}, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO node_share_commands(handoff_id,backend_id,payload,payload_sha256,receipt) VALUES(?,?,?,?,?)`, c.ID, backend, raw, digest, encoded); e != nil {
			return contracts.ShareObservation{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return contracts.ShareObservation{}, e
	}
	return out, nil
}

// nodeShareTx is an extra tenant/node cap, independent of the existing funded
// runtime/share/credit leases. Installing it never creates or extends a lease.
func nodeShareTx(ctx context.Context, tx *sql.Tx, use AuthorizationUse, kind string) (contracts.ShareHandoff, bool, error) {
	var c contracts.ShareHandoff
	var tenant string
	e := tx.QueryRowContext(ctx, `SELECT tenant_id FROM node_share_bindings WHERE node_id=? AND rule_id=?`, use.NodeID, use.RuleID).Scan(&tenant)
	if errors.Is(e, sql.ErrNoRows) {
		return c, false, nil
	}
	if e != nil {
		return c, false, e
	}
	var raw []byte
	if e = tx.QueryRowContext(ctx, `SELECT payload FROM node_share_heads WHERE tenant_id=? AND node_id=? AND kind=?`, tenant, use.NodeID, kind).Scan(&raw); e != nil {
		return c, true, ErrAuthorizationDenied
	}
	c, e = contracts.DecodeShareHandoff(raw)
	if e != nil {
		return c, true, e
	}
	if !use.TrustedTime || c.BootID != use.BootID || use.Earliest.Before(c.IssuedAt) || !use.Latest.Before(c.OwnershipUntil) || !use.Latest.Before(c.PolicyUntil) || !use.Latest.Before(c.AutonomyUntil) {
		return c, true, ErrAuthorityExpired
	}
	return c, true, nil
}
func checkNodeShareOpenTx(ctx context.Context, tx *sql.Tx, use AuthorizationUse) error {
	// Both kinds must exist before new admission; a prepared bootstrap never
	// becomes a runtime authorization just because the first CAS arrived.
	if _, _, e := nodeShareTx(ctx, tx, use, "rate"); e != nil {
		return e
	}
	c, bound, e := nodeShareTx(ctx, tx, use, "connections")
	if e != nil {
		return e
	}
	if !bound {
		return nil
	}
	var count int64
	if count, e = countNodeShareConnectionsTx(ctx, tx, c.TenantID, use.NodeID); e != nil {
		return e
	}
	if count >= c.Amount {
		return ErrAuthorizationDenied
	}
	return nil
}
func consumeNodeShareRateTx(ctx context.Context, tx *sql.Tx, use AuthorizationUse, n uint64) error {
	c, bound, e := nodeShareTx(ctx, tx, use, "rate")
	if e != nil {
		return e
	}
	if !bound {
		return nil
	}
	if c.BurstBytes < 1 || n > uint64(c.BurstBytes) {
		return ErrAuthorizationDenied
	}
	var tokens, tick int64
	var boot string
	e = tx.QueryRowContext(ctx, `SELECT tokens,last_tick,boot_id FROM node_share_buckets WHERE tenant_id=? AND node_id=?`, c.TenantID, use.NodeID).Scan(&tokens, &tick, &boot)
	if errors.Is(e, sql.ErrNoRows) {
		tokens = c.BurstBytes
		tick = use.Latest.UnixNano()
	} else if e != nil {
		return e
	}
	if boot != "" && boot != use.BootID {
		return ErrAuthorityHandoffRequired
	}
	tokens = min(tokens, c.BurstBytes)
	delta := use.Earliest.UnixNano() - tick
	if delta > 0 {
		if delta >= int64(time.Second) {
			tokens = c.BurstBytes
		} else {
			hi, lo := bits.Mul64(uint64(delta), uint64(c.Amount))
			earnedU, _ := bits.Div64(hi, lo, 8000000000)
			earned := int64(earnedU)
			tokens = min(c.BurstBytes, tokens+earned)
		}
	}
	if tokens < int64(n) {
		return &RateLimited{RetryAfter: time.Duration((int64(n)-tokens)*8000000000/c.Amount + 1)}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO node_share_buckets(tenant_id,node_id,boot_id,tokens,last_tick) VALUES(?,?,?,?,?) ON CONFLICT(tenant_id,node_id) DO UPDATE SET tokens=excluded.tokens,last_tick=excluded.last_tick`, c.TenantID, use.NodeID, use.BootID, tokens-int64(n), use.Latest.UnixNano())
	return e
}

// Unknown historical sessions conservatively count against every tenant until
// proved closed; known tenants never consume one another's allocation.
func countNodeShareConnectionsTx(ctx context.Context, tx *sql.Tx, tenant, node string) (int64, error) {
	var count int64
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM runtime_sessions r LEFT JOIN authorization_records a ON a.node_id=r.node_id AND a.authorization_id=r.grant_id LEFT JOIN node_share_bindings b ON b.node_id=r.node_id AND b.rule_id=a.rule_id WHERE r.node_id=? AND r.state='open' AND (b.tenant_id=? OR b.tenant_id IS NULL)`, node, tenant).Scan(&count)
	return count, e
}

// BindPlatformShareRule is invoked from authenticated, validated compiler
// Prepare before it can be acknowledged. Every platform rule then requires
// both node capacity heads, including a second rule reusing an existing owner.
// This only narrows admission; it creates no runtime or traffic authority.
func (s *Store) BindPlatformShareRule(ctx context.Context, tenant, node, rule string) error {
	if s.owner != "agent" || !validIssuerUUID(tenant) || !validIssuerUUID(node) || !validIssuerUUID(rule) {
		return ErrAuthorizationDenied
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	e = tx.QueryRowContext(ctx, `SELECT tenant_id FROM node_share_bindings WHERE node_id=? AND rule_id=?`, node, rule).Scan(&old)
	if e == nil {
		if old != tenant {
			return ErrAuthorizationDenied
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO node_share_bindings(node_id,rule_id,tenant_id) VALUES(?,?,?)`, node, rule, tenant); e != nil {
		return e
	}
	return tx.Commit()
}
