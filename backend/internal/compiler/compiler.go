package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type Member struct {
	ID            string `json:"id"`
	BootID        string `json:"boot_id"`
	PublicAddress string `json:"public_address"`
	Version       int64  `json:"version"`
	Generation    int64  `json:"generation"`
	LinkPort      int32  `json:"link_port"`
	Weight        int32  `json:"weight"`
	Priority      int32  `json:"priority"`
	Online        bool   `json:"online"`
}
type Group struct {
	ID               string   `json:"id"`
	OwnerKind        string   `json:"owner_kind"`
	Version          int64    `json:"version"`
	MemberSetVersion string   `json:"member_set_version"`
	Scheduling       string   `json:"scheduling"`
	Members          []Member `json:"members"`
}
type Grant struct {
	ID         string    `json:"id"`
	GroupID    string    `json:"group_id"`
	Version    int64     `json:"version"`
	Active     bool      `json:"active"`
	ValidFrom  time.Time `json:"valid_from"`
	ValidUntil time.Time `json:"valid_until"`
}
type Permit struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Version int64  `json:"version"`
	Allowed bool   `json:"allowed"`
}
type BillingSnapshot struct {
	ID              string `json:"id"`
	Version         int64  `json:"version"`
	MultiplierMilli int64  `json:"multiplier_milli"`
	RuleRevision    int64  `json:"rule_revision"`
	PathGeneration  int64  `json:"path_generation"`
}
type CompileInput struct {
	TenantID          string          `json:"tenant_id"`
	Rule              Rule            `json:"rule"`
	Revision          int64           `json:"revision"`
	SignerEpoch       int64           `json:"signer_epoch"`
	PathGeneration    int64           `json:"path_generation"`
	FencingGeneration int64           `json:"fencing_generation"`
	ValidAt           time.Time       `json:"valid_at"`
	ReverseAllowed    bool            `json:"reverse_allowed"`
	Groups            []Group         `json:"groups"`
	Grants            []Grant         `json:"grants"`
	Permits           []Permit        `json:"permits"`
	Billing           BillingSnapshot `json:"billing"`
}
type Segment struct {
	Hop           int    `json:"hop"`
	From          string `json:"from"`
	To            string `json:"to"`
	Direction     string `json:"direction"`
	PermitVersion int64  `json:"permit_version"`
}
type Intent struct {
	Rule            Rule      `json:"rule"`
	Role            string    `json:"role"`
	Hop             int       `json:"hop_index"`
	AllowedUpstream []string  `json:"allowed_upstream_node_ids"`
	NextMembers     []Member  `json:"next_members"`
	Segments        []Segment `json:"segments"`
	SessionStable   bool      `json:"session_stable"`
}
type Envelope struct {
	TenantID          string            `json:"tenant_id"`
	RuleID            string            `json:"rule_id"`
	RuleRevision      int64             `json:"rule_revision"`
	NodeID            string            `json:"node_id"`
	BootID            string            `json:"boot_id"`
	SignerEpoch       int64             `json:"signer_epoch"`
	PathGeneration    int64             `json:"path_generation"`
	FencingGeneration int64             `json:"fencing_generation"`
	BillingSnapshotID string            `json:"billing_snapshot_id"`
	InputDigest       string            `json:"input_digest"`
	InputVersions     map[string]string `json:"input_versions"`
	Intent            Intent            `json:"intent"`
}
type Artifact struct {
	NodeID    string
	Canonical []byte
	Digest    string
}
type Result struct {
	InputCanonical []byte
	InputDigest    string
	Artifacts      []Artifact
	Diagnostics    []Diagnostic
}

const SnapshotRequired Diagnostic = "RULE_BILLING_SNAPSHOT_REQUIRED"
const MemberMismatch Diagnostic = "RULE_MEMBER_SET_INVALID"

func intString(n int64) string { return strconv.FormatInt(n, 10) }

func canonID(s string) (string, error) {
	u, e := uuid.Parse(s)
	if e != nil || u == uuid.Nil {
		return "", Invalid
	}
	return u.String(), nil
}
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == hex.EncodeToString(b)
}
func hash(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }

// Compile consumes one fully pinned input. ValidAt is part of that input; no
// time.Now, database, DNS, latest-catalog read or mutation belongs here.
func Compile(ctx context.Context, in CompileInput) Result {
	fail := func(d Diagnostic) Result { return Result{Artifacts: []Artifact{}, Diagnostics: []Diagnostic{d}} }
	if ctx.Err() != nil {
		return fail(Invalid)
	}
	var e error
	in.TenantID, e = canonID(in.TenantID)
	if e != nil {
		return fail(Invalid)
	}
	in.Rule, e = NormalizeRule(in.Rule)
	if e != nil {
		return fail(Invalid)
	}
	if in.Revision < 1 || in.PathGeneration < 1 || in.FencingGeneration < 1 || in.SignerEpoch < 1 || in.ValidAt.IsZero() {
		return fail(Invalid)
	}
	in.ValidAt = in.ValidAt.UTC()
	in.Billing.ID, e = canonID(in.Billing.ID)
	if e != nil || in.Billing.Version < 1 || in.Billing.MultiplierMilli < 0 || in.Billing.RuleRevision != in.Revision || in.Billing.PathGeneration != in.PathGeneration {
		return fail(SnapshotRequired)
	}
	// Copy all caller slices before canonical sorting.
	in.Groups = append([]Group(nil), in.Groups...)
	in.Grants = append([]Grant(nil), in.Grants...)
	in.Permits = append([]Permit(nil), in.Permits...)
	groups := map[string]Group{}
	nodes := map[string]bool{}
	versions := map[string]string{}
	for i, g := range in.Groups {
		g.ID, e = canonID(g.ID)
		if e != nil || groups[g.ID].ID != "" || g.Version < 1 || !validDigest(g.MemberSetVersion) || (g.OwnerKind != "tenant" && g.OwnerKind != "platform") || len(g.Members) == 0 || len(g.Members) > 1024 {
			return fail(MemberMismatch)
		}
		if g.Scheduling != "dns" && g.Scheduling != "weighted_round_robin" && g.Scheduling != "primary_backup" {
			return fail(MemberMismatch)
		}
		g.Members = append([]Member(nil), g.Members...)
		for j, n := range g.Members {
			n.ID, e = canonID(n.ID)
			if e != nil || nodes[n.ID] {
				return fail(MemberMismatch)
			}
			nodes[n.ID] = true
			n.BootID, e = canonID(n.BootID)
			if e != nil || !n.Online || n.Version < 1 || n.Generation < 1 || n.LinkPort < 1 || n.LinkPort > 65535 || n.Weight < 1 || n.Weight > 10000 || n.Priority < 0 {
				return fail(MemberMismatch)
			}
			g.Members[j] = n
		}
		sort.Slice(g.Members, func(i, j int) bool { return g.Members[i].ID < g.Members[j].ID })
		in.Groups[i] = g
		groups[g.ID] = g
		versions["group/"+g.ID] = g.MemberSetVersion
	}
	if len(groups) != len(in.Rule.Path) {
		return fail(MemberMismatch)
	}
	for _, id := range in.Rule.Path {
		if groups[id].ID == "" {
			return fail(MemberMismatch)
		}
	}
	grantGroups := map[string]bool{}
	for i, g := range in.Grants {
		g.ID, e = canonID(g.ID)
		if e != nil {
			return fail(Unauthorized)
		}
		g.GroupID, e = canonID(g.GroupID)
		if e != nil || grantGroups[g.GroupID] || groups[g.GroupID].OwnerKind != "platform" || !g.Active || g.Version < 1 || in.ValidAt.Before(g.ValidFrom) || !in.ValidAt.Before(g.ValidUntil) {
			return fail(Unauthorized)
		}
		g.ValidFrom = g.ValidFrom.UTC()
		g.ValidUntil = g.ValidUntil.UTC()
		in.Grants[i] = g
		grantGroups[g.GroupID] = true
		versions["grant/"+g.GroupID] = g.ID + "/" + intString(g.Version)
	}
	for _, g := range in.Groups {
		if g.OwnerKind == "platform" && !grantGroups[g.ID] {
			return fail(Unauthorized)
		}
	}
	permits := map[string]Permit{}
	for i, p := range in.Permits {
		p.From, e = canonID(p.From)
		if e != nil {
			return fail(Unauthorized)
		}
		p.To, e = canonID(p.To)
		key := p.From + "/" + p.To
		if e != nil || p.From == p.To || permits[key].From != "" || p.Version < 1 || !p.Allowed || groups[p.From].ID == "" || groups[p.To].ID == "" {
			return fail(Unauthorized)
		}
		permits[key] = p
		in.Permits[i] = p
		versions["permit/"+key] = intString(p.Version)
	}
	public := func(id string) bool {
		for _, m := range groups[id].Members {
			if m.PublicAddress == "" {
				return false
			}
		}
		return true
	}
	segments := make([]Segment, 0, len(in.Rule.Directions))
	for i, d := range in.Rule.Directions {
		from, to := in.Rule.Path[i], in.Rule.Path[i+1]
		key := from + "/" + to
		p, has := permits[key]
		if !has && (groups[from].OwnerKind == "platform" || groups[to].OwnerKind == "platform") {
			return fail(Unauthorized)
		}
		if d == "auto" {
			d = "forward"
			if !public(to) {
				d = "reverse"
			}
		} else if d == "reverse" && public(to) && !in.ReverseAllowed {
			return fail(Unauthorized)
		}
		if d == "forward" && !public(to) || d == "reverse" && !public(from) {
			return fail(Unreachable)
		}
		segments = append(segments, Segment{Hop: i, From: from, To: to, Direction: d, PermitVersion: p.Version})
	}
	if !public(in.Rule.Path[0]) {
		return fail(Unreachable)
	}
	sort.Slice(in.Groups, func(i, j int) bool { return in.Groups[i].ID < in.Groups[j].ID })
	sort.Slice(in.Grants, func(i, j int) bool { return in.Grants[i].GroupID < in.Grants[j].GroupID })
	sort.Slice(in.Permits, func(i, j int) bool { return in.Permits[i].From+in.Permits[i].To < in.Permits[j].From+in.Permits[j].To })
	versions["billing/"+in.Billing.ID] = intString(in.Billing.Version)
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > 524288 {
		return fail(Invalid)
	}
	result := Result{InputCanonical: raw, InputDigest: hash(raw), Artifacts: []Artifact{}, Diagnostics: []Diagnostic{}}
	for hop, id := range in.Rule.Path {
		role := "relay"
		if hop == 0 {
			role = "entry"
		} else if hop == len(in.Rule.Path)-1 {
			role = "exit"
		}
		upstream := []string{}
		next := []Member{}
		if hop > 0 {
			for _, n := range groups[in.Rule.Path[hop-1]].Members {
				upstream = append(upstream, n.ID)
			}
		}
		if hop+1 < len(in.Rule.Path) {
			next = append(next, groups[in.Rule.Path[hop+1]].Members...)
		}
		for _, node := range groups[id].Members {
			if ctx.Err() != nil {
				return fail(Invalid)
			}
			env := Envelope{TenantID: in.TenantID, RuleID: in.Rule.ID, RuleRevision: in.Revision, NodeID: node.ID, BootID: node.BootID, SignerEpoch: in.SignerEpoch, PathGeneration: in.PathGeneration, FencingGeneration: in.FencingGeneration, BillingSnapshotID: in.Billing.ID, InputDigest: result.InputDigest, InputVersions: versions, Intent: Intent{Rule: in.Rule, Role: role, Hop: hop, AllowedUpstream: upstream, NextMembers: next, Segments: segments, SessionStable: true}}
			body, e := json.Marshal(env)
			if e != nil || len(body) > 524288 {
				return fail(Invalid)
			}
			result.Artifacts = append(result.Artifacts, Artifact{NodeID: node.ID, Canonical: body, Digest: hash(body)})
		}
	}
	sort.Slice(result.Artifacts, func(i, j int) bool { return result.Artifacts[i].NodeID < result.Artifacts[j].NodeID })
	return result
}

// DecodeArtifact accepts only canonical bytes, including nested field names and
// explicit empty arrays. Ambiguous or substituted bytes cannot share a digest.
func DecodeArtifact(raw []byte, digest string) (Envelope, error) {
	var result Envelope
	if len(raw) == 0 || len(raw) > 524288 || !validDigest(digest) || hash(raw) != digest {
		return result, Invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&result); e != nil {
		return Envelope{}, Invalid
	}
	encoded, e := json.Marshal(result)
	if e != nil || !bytes.Equal(encoded, raw) {
		return Envelope{}, Invalid
	}
	return result, nil
}
