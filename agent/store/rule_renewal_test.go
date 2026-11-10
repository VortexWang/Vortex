package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestPendingCommitRenewalRequiresExactPreparedExecutorCorrelation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	idBytes := []byte("pending-rule-001")
	id := fmt.Sprintf("%x", idBytes)
	payload := []byte("immutable rule artifact")
	digest := sha256.Sum256(payload)
	if _, err = s.SaveRuleConfigRequest(ctx, "rule", ConfigResult{TransactionID: id, Version: 2, FencingGeneration: 8, Payload: payload, PayloadSHA256: digest[:]}); err != nil {
		t.Fatal(err)
	}
	executor := "0123456789abcdef0123456789abcdef"
	if err = s.BindActivationExecutor(ctx, id, executor); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConfigResult(ctx, id, 2, "prepared", ""); err != nil {
		t.Fatal(err)
	}
	check := func(want bool, e string) {
		t.Helper()
		got, err := s.PendingRuleCommit(ctx, id, e)
		if err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	bind := func(c *controlv1.RuleCommand) {
		t.Helper()
		raw, err := proto.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindRuleCommand(ctx, c.TaskKey, id, raw); err != nil {
			t.Fatal(err)
		}
	}
	c := &controlv1.RuleCommand{Phase: "prepare", TaskKey: "prepare", TransactionId: idBytes, TargetRevision: 2, FencingGeneration: 8, IdempotencyKey: "prepare", ArtifactSha256: digest[:]}
	bind(c)
	check(false, executor)
	c.Phase = "query"
	c.TaskKey = "query"
	bind(c)
	check(false, executor)
	c.Phase = "commit"
	c.TaskKey = "wrong-artifact"
	c.ArtifactSha256 = make([]byte, 32)
	bind(c)
	check(false, executor)
	c.TaskKey = "wrong-fence"
	c.ArtifactSha256 = digest[:]
	c.FencingGeneration = 9
	bind(c)
	check(false, executor)
	c.TaskKey = "unstripped-payload"
	c.FencingGeneration = 8
	c.Payload = payload
	bind(c)
	check(false, executor)
	c.TaskKey = "unknown-field"
	c.Payload = nil
	c.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	bind(c)
	check(false, executor)
	c.TaskKey = "correct"
	c.ProtoReflect().SetUnknown(nil)
	bind(c)
	check(true, executor)
	check(false, "fedcba9876543210fedcba9876543210fe")
	if err = s.SaveConfigResult(ctx, id, 2, "committing", ""); err != nil {
		t.Fatal(err)
	}
	check(false, executor)
}
