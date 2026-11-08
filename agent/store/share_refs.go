package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

// ShareReference is the approved authorization 1.2.0 reference. It describes
// delegated capacity; it does not itself create bytes, an authorization or a
// backend source. All historical revisions are retained for existing sessions.
type ShareReference struct {
	ID             string    `json:"share_id"`
	ParentID       string    `json:"parent_share_id"`
	ParentRevision uint64    `json:"parent_revision"`
	SourceID       string    `json:"source_id"`
	IssuerID       string    `json:"allowed_issuer_hub_id"`
	NodeID         string    `json:"subject_node_id"`
	SignerEpoch    uint64    `json:"signer_epoch"`
	Generation     uint64    `json:"share_generation"`
	Revision       uint64    `json:"revision"`
	RuleID         string    `json:"rule_id"`
	RuleRevision   uint64    `json:"rule_revision"`
	PathGeneration uint64    `json:"path_generation"`
	RateBPS        uint64    `json:"rate_bps"`
	Connections    uint64    `json:"connections"`
	NotBefore      time.Time `json:"not_before"`
	ExpiresAt      time.Time `json:"expires_at"`
	HardDeadline   time.Time `json:"hard_deadline"`
}

func ValidateShareReference(r ShareReference) error {
	for _, id := range []string{r.ID, r.SourceID, r.IssuerID, r.NodeID} {
		if !validIssuerUUID(id) {
			return ErrAuthorizationDenied
		}
	}
	if r.ParentID == "" {
		if r.ParentRevision != 0 {
			return ErrAuthorizationDenied
		}
	} else if !validIssuerUUID(r.ParentID) || r.ParentRevision == 0 || r.ParentRevision > math.MaxInt64 {
		return ErrAuthorizationDenied
	}
	for _, n := range []uint64{r.SignerEpoch, r.Generation, r.Revision, r.RuleRevision, r.PathGeneration} {
		if n == 0 || n > math.MaxInt64 {
			return ErrAuthorizationDenied
		}
	}
	if r.RuleID == "" || len(r.RuleID) > 128 || !utf8.ValidString(r.RuleID) || r.RateBPS == 0 || r.RateBPS > 1000000000000 || r.Connections == 0 || r.Connections > 1000000 || r.NotBefore.IsZero() || !r.ExpiresAt.After(r.NotBefore) || r.ExpiresAt.After(r.HardDeadline) {
		return ErrAuthorizationDenied
	}
	for _, d := range []time.Time{r.NotBefore, r.ExpiresAt, r.HardDeadline} {
		if d.Location() != time.UTC || d.Year() < 1 || d.Year() > 9999 {
			return ErrAuthorizationDenied
		}
	}
	return nil
}

// InstallShareSourceAnchor is a trusted provisioning-adapter boundary, like
// InstallHubDelegation. Its backend identity must already be authenticated by
// the caller. NodeControl never calls it from an AuthorizationUpdate or a
// self-asserted root. Phase-5 fixtures provision finite roots through this
// explicit internal boundary; production provisioning belongs to Phase 6.
func (s *Store) InstallShareSourceAnchor(ctx context.Context, nodeID, backendID string, root ShareReference) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.installShareSourceAnchorTx(ctx, tx, nodeID, backendID, root); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) installShareSourceAnchorTx(ctx context.Context, tx *sql.Tx, nodeID, backendID string, root ShareReference) error {
	if (s.owner != "agent" && s.owner != "hub") || !validIssuerUUID(nodeID) || !validIssuerUUID(backendID) || root.ParentID != "" || root.ParentRevision != 0 {
		return ErrAuthorizationDenied
	}
	if err := ValidateShareReference(root); err != nil {
		return err
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	var old []byte
	var issuer string
	err = tx.QueryRowContext(ctx, `SELECT payload,backend_id FROM share_source_anchors WHERE node_id=? AND source_id=? AND revision=?`, nodeID, root.SourceID, strconv.FormatUint(root.Revision, 10)).Scan(&old, &issuer)
	if err == nil {
		if !bytes.Equal(old, raw) || issuer != backendID {
			return ErrAuthorizationUpdateConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var floor, owner string
	err = tx.QueryRowContext(ctx, `SELECT revision,backend_id FROM share_source_heads WHERE node_id=? AND source_id=?`, nodeID, root.SourceID).Scan(&floor, &owner)
	if err == nil {
		n, e := strconv.ParseUint(floor, 10, 63)
		if e != nil || root.Revision <= n || owner != backendID {
			return ErrAuthorizationGenerationStale
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_source_anchors(node_id,source_id,revision,backend_id,payload,payload_sha256) VALUES(?,?,?,?,?,?)`, nodeID, root.SourceID, strconv.FormatUint(root.Revision, 10), backendID, raw, digest[:]); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_source_heads(node_id,source_id,revision,backend_id) VALUES(?,?,?,?) ON CONFLICT(node_id,source_id) DO UPDATE SET revision=excluded.revision`, nodeID, root.SourceID, strconv.FormatUint(root.Revision, 10), backendID); err != nil {
		return err
	}
	return nil
}

func readShareTx(ctx context.Context, tx *sql.Tx, node, id string, revision uint64) (ShareReference, error) {
	var r ShareReference
	var raw, digest []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM share_references WHERE node_id=? AND share_id=? AND revision=?`, node, id, strconv.FormatUint(revision, 10)).Scan(&raw, &digest); err != nil {
		return r, err
	}
	sum := sha256.Sum256(raw)
	if !bytes.Equal(sum[:], digest) || json.Unmarshal(raw, &r) != nil || r.ID != id || r.Revision != revision {
		return ShareReference{}, ErrAuthorizationDenied
	}
	return r, ValidateShareReference(r)
}

func shareHeadTx(ctx context.Context, tx *sql.Tx, node, id string) (uint64, error) {
	var text string
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM share_reference_heads WHERE node_id=? AND share_id=?`, node, id).Scan(&text); err != nil {
		return 0, err
	}
	return strconv.ParseUint(text, 10, 63)
}

// checkShareChainTx validates every parent and a separately provisioned root.
// New admission also checks current revisions; existing sessions may drain
// against retained immutable revisions inside their original time boundary.
func checkShareChainTx(ctx context.Context, tx *sql.Tx, node, issuer string, leaf ShareReference, current bool) error {
	r := leaf
	seen := map[string]bool{}
	for depth := 0; depth < 32; depth++ {
		if err := ValidateShareReference(r); err != nil {
			return err
		}
		if seen[r.ID] || r.IssuerID != issuer {
			return ErrAuthorizationDenied
		}
		seen[r.ID] = true
		if current {
			head, err := shareHeadTx(ctx, tx, node, r.ID)
			if err != nil || head != r.Revision {
				return ErrAuthorizationGenerationStale
			}
		}
		if r.ParentID == "" {
			var raw, digest []byte
			if err := tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM share_source_anchors WHERE node_id=? AND source_id=? AND revision=?`, node, r.SourceID, strconv.FormatUint(r.Revision, 10)).Scan(&raw, &digest); err != nil {
				return ErrAuthorizationDenied
			}
			canonical, err := json.Marshal(r)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			if !bytes.Equal(sum[:], digest) || !bytes.Equal(raw, canonical) {
				return ErrAuthorizationDenied
			}
			if current {
				var text string
				if err := tx.QueryRowContext(ctx, `SELECT revision FROM share_source_heads WHERE node_id=? AND source_id=?`, node, r.SourceID).Scan(&text); err != nil || text != strconv.FormatUint(r.Revision, 10) {
					return ErrAuthorizationGenerationStale
				}
			}
			return nil
		}
		parent, err := readShareTx(ctx, tx, node, r.ParentID, r.ParentRevision)
		if err != nil {
			return ErrAuthorizationDenied
		}
		if r.SourceID != parent.SourceID || r.RuleID != parent.RuleID || r.RuleRevision != parent.RuleRevision || r.PathGeneration != parent.PathGeneration || r.SignerEpoch != parent.SignerEpoch || r.Generation != parent.Generation || r.RateBPS > parent.RateBPS || r.Connections > parent.Connections || r.NotBefore.Before(parent.NotBefore) || r.ExpiresAt.After(parent.ExpiresAt) || r.HardDeadline.After(parent.HardDeadline) {
			return ErrAuthorizationDenied
		}
		r = parent
	}
	return fmt.Errorf("%w: share chain depth exceeded", ErrAuthorizationDenied)
}

func persistShareReferencesTx(ctx context.Context, tx *sql.Tx, u AuthorizationUpdate) error {
	if u.ShareReferences == nil {
		return nil
	}
	if len(u.ShareReferences) > 4096 || !validIssuerUUID(u.IssuerID) {
		return ErrAuthorizationDenied
	}
	seen := map[string]bool{}
	for _, r := range u.ShareReferences {
		if err := ValidateShareReference(r); err != nil {
			return err
		}
		if seen[r.ID] || r.IssuerID != u.IssuerID || r.SignerEpoch != u.SignerEpoch || r.PathGeneration != u.PathGeneration {
			return ErrAuthorizationDenied
		}
		seen[r.ID] = true
		old, err := readShareTx(ctx, tx, u.NodeID, r.ID, r.Revision)
		if err == nil {
			before, _ := json.Marshal(old)
			after, _ := json.Marshal(r)
			if !bytes.Equal(before, after) {
				return ErrAuthorizationUpdateConflict
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		floor, e := shareHeadTx(ctx, tx, u.NodeID, r.ID)
		if e == nil && r.Revision > floor && r.Revision != floor+1 {
			return ErrAuthorizationGenerationStale
		}
		if e == nil && r.Revision < floor {
			return ErrAuthorizationGenerationStale
		}
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		raw, _ := json.Marshal(r)
		sum := sha256.Sum256(raw)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO share_references(node_id,share_id,revision,parent_id,parent_revision,rate_bps,connections,payload,payload_sha256) VALUES(?,?,?,?,?,?,?,?,?)`, u.NodeID, r.ID, strconv.FormatUint(r.Revision, 10), r.ParentID, strconv.FormatUint(r.ParentRevision, 10), strconv.FormatUint(r.RateBPS, 10), strconv.FormatUint(r.Connections, 10), raw, sum[:]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO share_reference_heads(node_id,share_id,revision) VALUES(?,?,?) ON CONFLICT(node_id,share_id) DO UPDATE SET revision=excluded.revision`, u.NodeID, r.ID, strconv.FormatUint(r.Revision, 10)); err != nil {
			return err
		}
	}
	for _, r := range u.ShareReferences {
		if err := checkShareChainTx(ctx, tx, u.NodeID, u.IssuerID, r, true); err != nil {
			return err
		}
		if r.ParentID == "" {
			continue
		}
		p, err := readShareTx(ctx, tx, u.NodeID, r.ParentID, r.ParentRevision)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT r.rate_bps,r.connections FROM share_references r WHERE r.node_id=? AND r.parent_id=? AND r.parent_revision=? AND (EXISTS(SELECT 1 FROM share_reference_heads h WHERE h.node_id=r.node_id AND h.share_id=r.share_id AND h.revision=r.revision) OR EXISTS(SELECT 1 FROM config_share_holds c WHERE c.node_id=r.node_id AND c.share_id=r.share_id AND c.revision=r.revision AND c.state='held') OR EXISTS(SELECT 1 FROM authorization_records a JOIN runtime_sessions s ON s.node_id=a.node_id AND (s.initial_grant_id=a.authorization_id OR s.initial_runtime_lease_id=a.authorization_id OR s.initial_share_lease_id=a.authorization_id) WHERE a.node_id=r.node_id AND a.share_reference_id=r.share_id AND a.share_revision=r.revision AND s.state='open'))`, u.NodeID, r.ParentID, strconv.FormatUint(r.ParentRevision, 10))
		if err != nil {
			return err
		}
		var rate, connections uint64
		for rows.Next() {
			var rateText, connText string
			if err := rows.Scan(&rateText, &connText); err != nil {
				rows.Close()
				return err
			}
			n, e1 := strconv.ParseUint(rateText, 10, 63)
			c, e2 := strconv.ParseUint(connText, 10, 63)
			if e1 != nil || e2 != nil || n > p.RateBPS-rate || c > p.Connections-connections {
				rows.Close()
				return ErrAuthorizationDenied
			}
			rate += n
			connections += c
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func checkAuthorizationShareTx(ctx context.Context, tx *sql.Tx, node string, a AuthorizationRecord, current bool) error {
	if a.ShareReferenceID == "" && a.DelegationID == "" {
		return nil
	} // explicitly legacy records only
	if !validIssuerUUID(a.ShareReferenceID) || !validIssuerUUID(a.DelegationID) || a.DelegationID != a.ParentAuthorizationID {
		return ErrAuthorizationDenied
	}
	var revision uint64
	// The authorization stores the immutable revision it was validated against.
	revision = a.ShareRevision
	leaf, err := readShareTx(ctx, tx, node, a.ShareReferenceID, revision)
	if err != nil {
		return ErrAuthorizationDenied
	}
	if leaf.NodeID != node || leaf.SourceID != a.SourceID || leaf.RuleID != a.RuleID || leaf.RuleRevision != a.RuleRevision || leaf.PathGeneration != a.PathGeneration || leaf.SignerEpoch != a.SignerEpoch {
		return ErrAuthorizationDenied
	}
	notBefore, e1 := time.Parse(time.RFC3339Nano, a.NotBefore)
	expires, e2 := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	hard, e3 := time.Parse(time.RFC3339Nano, a.HardDeadline)
	if e1 != nil || e2 != nil || e3 != nil || notBefore.Before(leaf.NotBefore) || expires.After(leaf.ExpiresAt) || hard.After(leaf.HardDeadline) || (a.Kind == "share_lease" && (a.RateBPS > leaf.RateBPS || a.Connections > leaf.Connections)) {
		return ErrAuthorizationDenied
	}
	return checkShareChainTx(ctx, tx, node, a.IssuerID, leaf, current)
}
