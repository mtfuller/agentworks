package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertMatchesSchema validates a command's real --json output against the
// published schema in docs/schemas/, so the schema is a tested contract and not
// just documentation. name is the schema's file stem ("validate", "error", ...).
func assertMatchesSchema(t *testing.T, name string, stdout []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "schemas", name+".schema.json"))
	if err != nil {
		t.Fatalf("reading schema %s: %v", name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parsing schema %s: %v", name, err)
	}
	var doc any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("%s output is not JSON: %v\n%s", name, err, stdout)
	}
	if err := validateAgainst(schema, doc, "$"); err != nil {
		t.Errorf("%s output violates docs/schemas/%s.schema.json: %v\noutput: %s", name, name, err, stdout)
	}
}

// validateAgainst checks v against the small subset of JSON Schema the
// generated schemas use: type, const, required, properties, and items.
// Additional properties are allowed, as the schemas promise.
func validateAgainst(schema map[string]any, v any, path string) error {
	if want, ok := schema["const"]; ok && fmt.Sprint(want) != fmt.Sprint(v) {
		return fmt.Errorf("%s = %v, want the constant %v", path, v, want)
	}

	switch schema["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is %s, want an object", path, kindOf(v))
		}
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, present := obj[r.(string)]; !present {
					return fmt.Errorf("%s is missing required property %q", path, r)
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for name, sub := range props {
			if val, present := obj[name]; present {
				if err := validateAgainst(sub.(map[string]any), val, path+"."+name); err != nil {
					return err
				}
			}
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s is %s, want an array (a null slice is a bug)", path, kindOf(v))
		}
		items, _ := schema["items"].(map[string]any)
		for i, item := range list {
			if err := validateAgainst(items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s is %s, want a string", path, kindOf(v))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s is %s, want a boolean", path, kindOf(v))
		}
	case "integer":
		n, ok := v.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("%s is %s, want an integer", path, kindOf(v))
		}
	}
	return nil
}

func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", v), "*")
}

func TestSchemaValidatorCatchesViolations(t *testing.T) {
	schema := map[string]any{
		"type": "object", "required": []any{"ok", "items"},
		"properties": map[string]any{
			"ok":    map[string]any{"type": "boolean"},
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	cases := map[string]any{
		"missing required": map[string]any{"ok": true},
		"wrong type":       map[string]any{"ok": "yes", "items": []any{}},
		"null array":       map[string]any{"ok": true, "items": nil},
		"bad item":         map[string]any{"ok": true, "items": []any{1.0}},
	}
	for name, doc := range cases {
		if err := validateAgainst(schema, doc, "$"); err == nil {
			t.Errorf("%s: validator accepted an invalid document", name)
		}
	}
	if err := validateAgainst(schema, map[string]any{"ok": true, "items": []any{"a"}, "extra": 1.0}, "$"); err != nil {
		t.Errorf("a valid document with an extra property was rejected: %v", err)
	}
}
