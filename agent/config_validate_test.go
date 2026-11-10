package agent

import (
	"strings"
	"testing"
)

func TestLegacyRuntimeSchemaBranchPreservesControlValidation(t *testing.T) {
	node := "550e8400-e29b-41d4-a716-446655440000"
	if err := validateRuntimeConfig([]byte(validRuntimePayload), node, 2); err != nil {
		t.Fatalf("published schema rejected unchanged legacy input: %v", err)
	}
	for _, payload := range []string{
		strings.TrimSuffix(validRuntimePayload, "}") + `,"unexpected":true}`,
		strings.Replace(validRuntimePayload, `"listeners":[]`, `"listeners":"invalid"`, 1),
		`{"schema_version":"1.2.0","rule_id":"550e8400-e29b-41d4-a716-446655440001","revision":2}`,
	} {
		if err := validateRuntimeConfig([]byte(payload), node, 2); err == nil {
			t.Fatalf("legacy control path accepted invalid or rule-branch input: %s", payload)
		}
	}
}

func TestLegacyRuntimeSchemaRejectsUnsupportedBranches(t *testing.T) {
	for _, raw := range []string{
		`{"oneOf":[{"$ref":"#/$defs/legacy"}],"$defs":{"legacy":{"type":"object"}}}`,
		`{"oneOf":[{"$ref":"#/$defs/rule_artifact"},{"$ref":"#/$defs/legacy"}],"$defs":{"legacy":{"type":"object"},"rule_artifact":{"type":"object"}}}`,
		`{"oneOf":[{"$ref":"#/$defs/legacy"},{"$ref":"#/$defs/rule_artifact"}],"$defs":{"legacy":{"type":"object"}}}`,
	} {
		if _, err := legacyRuntimeSchema([]byte(raw)); err == nil {
			t.Fatal("accepted unsupported or incomplete runtime schema branch selection")
		}
	}
}
