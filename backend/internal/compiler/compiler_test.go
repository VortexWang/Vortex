package compiler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/testutil"
)

func TestCompileArtifactImmutableSchema(t *testing.T) {
	pool := testutil.Postgres(t)
	var exists bool
	if e := pool.QueryRow(context.Background(), "SELECT to_regclass('rule_artifacts') IS NOT NULL").Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		t.Fatal("immutable per-node compiled artifact storage missing")
	}
}

func compileFixture() compiler.CompileInput {
	const entry = "00000000-0000-4000-8000-000000000001"
	const exit = "00000000-0000-4000-8000-000000000002"
	return compiler.CompileInput{TenantID: "00000000-0000-4000-8000-000000000003", Rule: compiler.Rule{ID: "00000000-0000-4000-8000-000000000004", Name: "重放规则", Protocol: "tcp_udp", Mode: "tunnel", Port: 20000, Path: []string{entry, exit}, Directions: []string{"auto"}, Targets: []compiler.Target{{Address: "EXAMPLE.COM.:443", Weight: 2}, {Address: "192.0.2.80:443", Weight: 1}}, Scheduling: "weighted_round_robin", ProxyProtocol: "v2", Enabled: true}, Revision: 7, SignerEpoch: 11, PathGeneration: 13, FencingGeneration: 17, ValidAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), Billing: compiler.BillingSnapshot{ID: "00000000-0000-4000-8000-000000000005", Version: 19, MultiplierMilli: 1500, RuleRevision: 7, PathGeneration: 13}, Groups: []compiler.Group{
		{ID: entry, OwnerKind: "tenant", Version: 2, MemberSetVersion: strings.Repeat("a", 64), Scheduling: "dns", Members: []compiler.Member{{ID: "00000000-0000-4000-8000-000000000010", BootID: "00000000-0000-4000-8000-000000000020", PublicAddress: "192.0.2.10", Version: 3, Generation: 5, LinkPort: 443, Weight: 2, Online: true}, {ID: "00000000-0000-4000-8000-000000000011", BootID: "00000000-0000-4000-8000-000000000021", PublicAddress: "192.0.2.11", Version: 3, Generation: 5, LinkPort: 443, Weight: 1, Online: true}}},
		{ID: exit, OwnerKind: "tenant", Version: 2, MemberSetVersion: strings.Repeat("b", 64), Scheduling: "weighted_round_robin", Members: []compiler.Member{{ID: "00000000-0000-4000-8000-000000000012", BootID: "00000000-0000-4000-8000-000000000022", PublicAddress: "", Version: 3, Generation: 5, LinkPort: 443, Weight: 1, Online: true}}},
	}}
}

func TestCompileCanonicalDigestReplay(t *testing.T) {
	in := compileFixture()
	before, _ := json.Marshal(in)
	a := compiler.Compile(context.Background(), in)
	if len(a.Diagnostics) != 0 || len(a.Artifacts) != 3 {
		t.Fatal(a.Diagnostics)
	}
	after, _ := json.Marshal(in)
	if !bytes.Equal(before, after) {
		t.Fatal("compiler mutated input")
	}
	in.Groups[0], in.Groups[1] = in.Groups[1], in.Groups[0]
	in.Groups[1].Members[0], in.Groups[1].Members[1] = in.Groups[1].Members[1], in.Groups[1].Members[0]
	in.Rule.Targets[0], in.Rule.Targets[1] = in.Rule.Targets[1], in.Rule.Targets[0]
	b := compiler.Compile(context.Background(), in)
	if !bytes.Equal(a.InputCanonical, b.InputCanonical) || a.InputDigest != b.InputDigest {
		t.Fatal("input order changed canonical bytes")
	}
	for i, artifact := range a.Artifacts {
		if artifact.Digest != b.Artifacts[i].Digest || !bytes.Equal(artifact.Canonical, b.Artifacts[i].Canonical) || artifact.Digest != strings.ToLower(artifact.Digest) {
			t.Fatal("artifact order nondeterministic")
		}
		envelope, e := compiler.DecodeArtifact(artifact.Canonical, artifact.Digest)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(envelope)
		if e != nil || !bytes.Equal(raw, artifact.Canonical) {
			t.Fatal("decode/reencode changed canonical bytes", e)
		}
		if envelope.SignerEpoch != 11 || envelope.PathGeneration != 13 || envelope.FencingGeneration != 17 || envelope.BillingSnapshotID != in.Billing.ID || !envelope.Intent.SessionStable || envelope.Intent.Segments[0].Direction != "reverse" {
			t.Fatal("authority boundaries collapsed", envelope)
		}
		if _, e = compiler.DecodeArtifact(append(append([]byte(nil), artifact.Canonical...), byte(' ')), artifact.Digest); e == nil {
			t.Fatal("noncanonical substitution accepted")
		}
	}
	var replay compiler.CompileInput
	if e := json.Unmarshal(a.InputCanonical, &replay); e != nil {
		t.Fatal(e)
	}
	c := compiler.Compile(context.Background(), replay)
	if c.InputDigest != a.InputDigest || !bytes.Equal(c.Artifacts[0].Canonical, a.Artifacts[0].Canonical) {
		t.Fatal("pinned replay differs")
	}
	// Runtime-config 1.2 adds the pinned upstream incarnation/address set;
	// input bytes remain identical, while the versioned artifact gains fields.
	if a.InputDigest != "486471a55d696f7d1a549c10036c51140e6f670a865356f26d2e0e11853b958f" || a.Artifacts[0].Digest != "4de67e9b880280ff7f44860dda9f0bb859e5f644ea8f3026fc91b3f9aeadf232" {
		t.Fatal("golden replay changed", a.InputDigest, a.Artifacts[0].Digest)
	}
}

func TestCompileInvalidAuthorityNoArtifacts(t *testing.T) {
	for _, mutate := range []func(*compiler.CompileInput){
		func(i *compiler.CompileInput) { i.Billing.ID = "" }, func(i *compiler.CompileInput) { i.Billing.RuleRevision++ }, func(i *compiler.CompileInput) { i.Groups[0].OwnerKind = "platform" }, func(i *compiler.CompileInput) { i.Groups[0].Members[0].Online = false }, func(i *compiler.CompileInput) { i.Groups[1].Members[0].ID = i.Groups[0].Members[0].ID }, func(i *compiler.CompileInput) { i.Rule.Path = append(i.Rule.Path, i.Rule.Path[0]) }, func(i *compiler.CompileInput) { i.Groups[0].MemberSetVersion = "" }, func(i *compiler.CompileInput) {
			i.Permits = []compiler.Permit{{From: i.Rule.Path[0], To: i.Rule.Path[1], Version: 1, Allowed: false}}
		},
	} {
		in := compileFixture()
		mutate(&in)
		r := compiler.Compile(context.Background(), in)
		if len(r.Diagnostics) != 1 || len(r.Artifacts) != 0 || len(r.InputCanonical) != 0 {
			t.Fatal("invalid input produced publishable artifact", r)
		}
	}
}

func TestCompileGrantPermitAndDirection(t *testing.T) {
	in := compileFixture()
	in.Groups[0].OwnerKind = "platform"
	in.Grants = []compiler.Grant{{ID: "00000000-0000-4000-8000-000000000099", GroupID: in.Groups[0].ID, Version: 4, Active: true, ValidFrom: in.ValidAt.Add(-time.Hour), ValidUntil: in.ValidAt.Add(time.Hour)}}
	r := compiler.Compile(context.Background(), in)
	if len(r.Diagnostics) != 1 || r.Diagnostics[0] != compiler.Unauthorized {
		t.Fatal("grant implied independent segment permit")
	}
	in.Permits = []compiler.Permit{{From: in.Rule.Path[0], To: in.Rule.Path[1], Version: 6, Allowed: true}}
	r = compiler.Compile(context.Background(), in)
	if len(r.Diagnostics) != 0 || len(r.Artifacts) != 3 {
		t.Fatal("valid fixed grant/permit rejected", r.Diagnostics)
	}
	in.ValidAt = in.Grants[0].ValidUntil
	r = compiler.Compile(context.Background(), in)
	if len(r.Artifacts) != 0 {
		t.Fatal("expiry boundary allowed grant")
	}
}
