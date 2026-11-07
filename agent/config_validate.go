package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"vortex/contracts"
)

const maxRuntimeConfigBytes = 1 << 20

type jsonSchema struct {
	Type                 string                `json:"type"`
	Format               string                `json:"format"`
	Required             []string              `json:"required"`
	Properties           map[string]jsonSchema `json:"properties"`
	Items                *jsonSchema           `json:"items"`
	AdditionalProperties *bool                 `json:"additionalProperties"`
	Enum                 []any                 `json:"enum"`
	Const                any                   `json:"const"`
	Minimum              *float64              `json:"minimum"`
	Maximum              *float64              `json:"maximum"`
}

var (
	runtimeSchemaOnce sync.Once
	runtimeSchema     jsonSchema
	runtimeSchemaErr  error
)

func validateRuntimeConfig(payload []byte, nodeID string, version uint64) error {
	if len(payload) == 0 || len(payload) > maxRuntimeConfigBytes || !utf8.Valid(payload) {
		return fmt.Errorf("invalid_config: payload must be UTF-8 JSON of 1..%d bytes", maxRuntimeConfigBytes)
	}
	runtimeSchemaOnce.Do(func() {
		runtimeSchema, runtimeSchemaErr = legacyRuntimeSchema(contracts.RuntimeConfigSchema)
	})
	if runtimeSchemaErr != nil {
		return fmt.Errorf("invalid_config: runtime schema unavailable: %w", runtimeSchemaErr)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("invalid_config: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid_config: multiple JSON values")
	}
	if err := validateSchemaValue(value, runtimeSchema, "$ "); err != nil {
		return fmt.Errorf("invalid_config: %w", err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid_config: root must be an object")
	}
	if got, ok := root["node_id"].(string); !ok || got != nodeID {
		return fmt.Errorf("identity_mismatch: runtime node_id does not match authenticated node")
	}
	rawVersion, ok := root["version"].(json.Number)
	if !ok {
		return fmt.Errorf("invalid_config: version must be an integer")
	}
	gotVersion, err := strconv.ParseUint(string(rawVersion), 10, 64)
	if err != nil || gotVersion != version {
		return fmt.Errorf("stale_version: envelope and payload versions do not match")
	}
	return nil
}

// Legacy control updates must keep the original schema, rather than accepting
// the separately negotiated rule artifact branch of the published contract.
func legacyRuntimeSchema(raw []byte) (jsonSchema, error) {
	var document struct {
		OneOf []struct {
			Ref string `json:"$ref"`
		} `json:"oneOf"`
		Definitions map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return jsonSchema{}, err
	}
	if len(document.OneOf) != 0 {
		if len(document.OneOf) != 2 || document.OneOf[0].Ref != "#/$defs/legacy" || document.OneOf[1].Ref != "#/$defs/rule_artifact" || len(document.Definitions["legacy"]) == 0 || len(document.Definitions["rule_artifact"]) == 0 {
			return jsonSchema{}, fmt.Errorf("unsupported runtime schema branches")
		}
		raw = document.Definitions["legacy"]
	}
	var schema jsonSchema
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&schema); err != nil {
		return jsonSchema{}, err
	}
	if schema.Type != "object" {
		return jsonSchema{}, fmt.Errorf("legacy runtime schema must be an object")
	}
	return schema, nil
}

func validateSchemaValue(value any, schema jsonSchema, path string) error {
	switch schema.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", strings.TrimSpace(path))
		}
		for _, key := range schema.Required {
			if _, ok := object[key]; !ok {
				return fmt.Errorf("%s.%s is required", strings.TrimSpace(path), key)
			}
		}
		for key, child := range object {
			property, exists := schema.Properties[key]
			if !exists {
				if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
					return fmt.Errorf("%s.%s is not supported", strings.TrimSpace(path), key)
				}
				continue
			}
			if err := validateSchemaValue(child, property, strings.TrimSpace(path)+"."+key); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", strings.TrimSpace(path))
		}
		if schema.Items != nil {
			for i, item := range array {
				if err := validateSchemaValue(item, *schema.Items, fmt.Sprintf("%s[%d]", strings.TrimSpace(path), i)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", strings.TrimSpace(path))
		}
		if schema.Format == "uuid" && !canonicalUUID(text) {
			return fmt.Errorf("%s must be a canonical UUID", strings.TrimSpace(path))
		}
		if len(schema.Enum) > 0 && !schemaContains(schema.Enum, text) {
			return fmt.Errorf("%s has an unsupported value", strings.TrimSpace(path))
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(string(number), ".eE") {
			return fmt.Errorf("%s must be an integer", strings.TrimSpace(path))
		}
		f, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("%s is outside the supported integer range", strings.TrimSpace(path))
		}
		if schema.Minimum != nil && f < *schema.Minimum || schema.Maximum != nil && f > *schema.Maximum {
			return fmt.Errorf("%s is outside its allowed range", strings.TrimSpace(path))
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", strings.TrimSpace(path))
		}
		f, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("%s is not a finite number", strings.TrimSpace(path))
		}
		if schema.Minimum != nil && f < *schema.Minimum || schema.Maximum != nil && f > *schema.Maximum {
			return fmt.Errorf("%s is outside its allowed range", strings.TrimSpace(path))
		}
	case "":
		if len(schema.Enum) == 0 && schema.Const == nil {
			return fmt.Errorf("schema has unconstrained value at %s", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", strings.TrimSpace(path))
		}
	default:
		return fmt.Errorf("schema has unsupported type %q", schema.Type)
	}
	if len(schema.Enum) > 0 && !schemaContains(schema.Enum, value) {
		return fmt.Errorf("%s has an unsupported value", strings.TrimSpace(path))
	}
	if schema.Const != nil && !schemaValueEqual(value, schema.Const) {
		return fmt.Errorf("%s does not match its required constant", strings.TrimSpace(path))
	}
	return nil
}

func schemaContains(values []any, value any) bool {
	for _, candidate := range values {
		if schemaValueEqual(candidate, value) {
			return true
		}
	}
	return false
}

func schemaValueEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func canonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
