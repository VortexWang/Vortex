package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/httpapi"
)

var contractOnce sync.Once
var contractDocument map[string]any
var contractLoadError error

func publicContract(t *testing.T) map[string]any {
	t.Helper()
	contractOnce.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		raw, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../contracts/openapi/v1/openapi.yaml"))
		if e != nil {
			contractLoadError = e
			return
		}
		contractLoadError = json.Unmarshal(raw, &contractDocument)
	})
	if contractLoadError != nil {
		t.Fatal(contractLoadError)
	}
	return contractDocument
}
func resolveContract(d map[string]any, s map[string]any) map[string]any {
	if r, ok := s["$ref"].(string); ok {
		var x any = d
		for _, part := range strings.Split(strings.TrimPrefix(r, "#/"), "/") {
			x = x.(map[string]any)[part]
		}
		return x.(map[string]any)
	}
	return s
}
func schemaError(d map[string]any, s map[string]any, value any, where string) error {
	s = resolveContract(d, s)
	if variants, ok := s["oneOf"].([]any); ok {
		matched := 0
		for _, v := range variants {
			if schemaError(d, v.(map[string]any), value, where) == nil {
				matched++
			}
		}
		if matched != 1 {
			return fmt.Errorf("%s: oneOf matched%d", where, matched)
		}
		return nil
	}
	if variants, ok := s["type"].([]any); ok {
		for _, v := range variants {
			c := map[string]any{}
			for k, x := range s {
				c[k] = x
			}
			c["type"] = v
			if schemaError(d, c, value, where) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: no matching type", where)
	}
	bad := func() error { return fmt.Errorf("%s: value%v violates schema%v", where, value, s) }
	switch s["type"] {
	case "null":
		if value != nil {
			return bad()
		}
	case "object":
		v, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		p, _ := s["properties"].(map[string]any)
		if r, ok := s["required"].([]any); ok {
			for _, n := range r {
				if _, ok := v[n.(string)]; !ok {
					return fmt.Errorf("%s: missing%s", where, n)
				}
			}
		}
		for k, x := range v {
			if property, ok := p[k].(map[string]any); ok {
				if e := schemaError(d, property, x, where+"."+k); e != nil {
					return e
				}
			} else if s["additionalProperties"] == false {
				return fmt.Errorf("%s: undeclared%s", where, k)
			} else if a, ok := s["additionalProperties"].(map[string]any); ok {
				if e := schemaError(d, a, x, where+"."+k); e != nil {
					return e
				}
			}
		}
	case "array":
		v, ok := value.([]any)
		if !ok {
			return bad()
		}
		if n, ok := s["minItems"].(float64); ok && len(v) < int(n) {
			return bad()
		}
		if n, ok := s["maxItems"].(float64); ok && len(v) > int(n) {
			return bad()
		}
		for i, x := range v {
			if e := schemaError(d, s["items"].(map[string]any), x, fmt.Sprintf("%s[%d]", where, i)); e != nil {
				return e
			}
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return bad()
		}
		if p, ok := s["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(v) {
			return bad()
		}
		if s["format"] == "uuid" {
			if _, e := uuid.Parse(v); e != nil {
				return bad()
			}
		}
		if s["format"] == "date-time" {
			if _, e := time.Parse(time.RFC3339Nano, v); e != nil {
				return bad()
			}
		}
	case "integer":
		v, ok := value.(float64)
		if !ok || float64(int64(v)) != v {
			return bad()
		}
		if n, ok := s["minimum"].(float64); ok && v < n {
			return bad()
		}
		if n, ok := s["maximum"].(float64); ok && v > n {
			return bad()
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad()
		}
	}
	if choices, ok := s["enum"].([]any); ok {
		for _, choice := range choices {
			if reflect.DeepEqual(choice, value) {
				return nil
			}
		}
		return bad()
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, value) {
		return bad()
	}
	return nil
}
func assertPublicContractResponse(t *testing.T, method, path string, w *httptest.ResponseRecorder) {
	t.Helper()
	d := publicContract(t)
	u, e := url.Parse(path)
	if e != nil {
		t.Fatal(e)
	}
	for template, item := range d["paths"].(map[string]any) {
		expression := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(template, `[^/]+`)
		if !regexp.MustCompile("^" + expression + "$").MatchString(u.Path) {
			continue
		}
		operation, ok := item.(map[string]any)[strings.ToLower(method)].(map[string]any)
		if !ok {
			continue
		}
		response, ok := operation["responses"].(map[string]any)[strconv.Itoa(w.Code)].(map[string]any)
		if !ok {
			t.Fatalf("%s%s undocumented status%d: %s", method, path, w.Code, w.Body)
		}
		response = resolveContract(d, response)
		content, ok := response["content"].(map[string]any)
		if !ok {
			if w.Body.Len() != 0 {
				t.Fatal("unexpected response body", w.Body)
			}
			return
		}
		media, ok := content["application/json"].(map[string]any)
		if !ok {
			return
		}
		var body any
		if e = json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		if e = schemaError(d, media["schema"].(map[string]any), body, method+" "+template); e != nil {
			t.Fatal(e)
		}
		return
	}
}
func TestOpenAPIExactlyMatchesRegisteredGinRoutes(t *testing.T) {
	d := publicContract(t)
	codec, e := httpapi.NewCursorCodec([]byte(strings.Repeat("contract-only-key", 3)), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	h, e := httpapi.NewRouter(httpapi.Config{Pool: &pgxpool.Pool{}, Tokens: &auth.Tokens{}, Cursors: codec})
	if e != nil {
		t.Fatal(e)
	}
	actual := map[string]bool{}
	for _, r := range h.(*gin.Engine).Routes() {
		parts := strings.Split(r.Path, "/")
		for i, p := range parts {
			if strings.HasPrefix(p, ":") {
				parts[i] = "{" + strings.TrimPrefix(p, ":") + "}"
			}
		}
		actual[r.Method+" "+strings.Join(parts, "/")] = true
	}
	count := 0
	for path, item := range d["paths"].(map[string]any) {
		for method := range item.(map[string]any) {
			if method == "parameters" || strings.HasPrefix(method, "x-") {
				continue
			}
			key := strings.ToUpper(method) + " " + path
			if !actual[key] {
				t.Error("documented route has no implementation", key)
			}
			delete(actual, key)
			count++
		}
	}
	for key := range actual {
		t.Error("registered route absent from contract", key)
	}
	if count < 40 {
		t.Fatal("incomplete business boundary", count)
	}
}
func TestOpenAPIResponseValidatorRejectsInternalAndMissingFields(t *testing.T) {
	d := publicContract(t)
	s := d["components"].(map[string]any)["schemas"].(map[string]any)["NodeSummary"].(map[string]any)
	for _, v := range []any{map[string]any{}, map[string]any{"id": uuid.NewString(), "boot_id": uuid.NewString()}} {
		if schemaError(d, s, v, "node") == nil {
			t.Fatal("accepted invalid public shape")
		}
	}
}
