package compiler_test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"testing"
	"vortex/backend/internal/compiler"
	runtimerule "vortex/engine/runtime/rule"
)

// Exercise the real compiler output against the published bounded shape.
// Semantic and byte-canonical validation remain the runtime's responsibility.
func TestCompileRuntimeSchemaCompatibility(t *testing.T) {
	for _, hops := range []int{1, 2, 3, 16} {
		t.Run(fmt.Sprintf("hops-%d", hops), func(t *testing.T) {
			checkRuntimeSchema(t, compiler.Compile(context.Background(), schemaFixture(hops)))
		})
	}
}

func schemaFixture(hops int) compiler.CompileInput {
	in := compileFixture()
	in.Rule.Path = nil
	in.Rule.Directions = nil
	in.Groups = nil
	for i := 0; i < hops; i++ {
		group := compileFixture().Groups[0]
		group.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+i)
		group.Members = group.Members[:1]
		group.Members[0].ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", 200+i)
		group.Members[0].BootID = fmt.Sprintf("00000000-0000-4000-8000-%012d", 300+i)
		in.Groups = append(in.Groups, group)
		in.Rule.Path = append(in.Rule.Path, group.ID)
	}
	in.Rule.Mode = "multi_hop"
	if hops == 1 {
		in.Rule.Mode = "direct"
	} else if hops == 2 {
		in.Rule.Mode = "tunnel"
	}
	return in
}

func checkRuntimeSchema(t *testing.T, r compiler.Result) {
	t.Helper()
	if len(r.Diagnostics) != 0 || len(r.Artifacts) == 0 {
		t.Fatal(r.Diagnostics)
	}
	for _, a := range r.Artifacts {
		if _, err := runtimerule.Decode(a.Canonical); err != nil {
			t.Fatalf("compiler artifact rejected by real runtime: %v", err)
		}
		cmd := exec.Command("python3", "../../../tools/phase6/rule_schema_check.py")
		cmd.Stdin = bytes.NewReader(a.Canonical)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("compiler artifact does not match candidate schema: %v\n%s", err, out)
		}
	}
}

func TestCompileRuntimeBounds(t *testing.T) {
	t.Run("path-overflow", func(t *testing.T) {
		r := compiler.Compile(context.Background(), schemaFixture(17))
		if len(r.Artifacts) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0] != compiler.Invalid {
			t.Fatal("unexecutable path produced artifact", r.Diagnostics)
		}
	})
	for _, size := range []int{256, 257} {
		t.Run(fmt.Sprintf("members-%d", size), func(t *testing.T) {
			in := schemaFixture(2)
			member := in.Groups[0].Members[0]
			in.Groups[0].Members = nil
			for i := 0; i < size; i++ {
				m := member
				m.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", 1000+i)
				m.BootID = fmt.Sprintf("00000000-0000-4000-8000-%012d", 2000+i)
				in.Groups[0].Members = append(in.Groups[0].Members, m)
			}
			r := compiler.Compile(context.Background(), in)
			if size == 257 {
				if len(r.Artifacts) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0] != compiler.MemberMismatch {
					t.Fatal("unexecutable member set produced artifact", r.Diagnostics)
				}
				return
			}
			if len(r.Artifacts) != size+1 || len(r.Diagnostics) != 0 {
				t.Fatal("runtime member limit rejected", r.Diagnostics)
			}
			// The exit artifact contains the entire upstream member set.
			for _, a := range r.Artifacts {
				if a.NodeID == in.Groups[1].Members[0].ID {
					checkRuntimeSchema(t, compiler.Result{Artifacts: []compiler.Artifact{a}})
				}
			}
		})
	}
}
