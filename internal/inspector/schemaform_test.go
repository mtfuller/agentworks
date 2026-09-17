package inspector

import (
	"encoding/json"
	"testing"

	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// testTool builds a minimal mcpclient.Tool around a raw inputSchema, for
// tests that only care about the schema -> form translation, not a real
// name/description.
func testTool(schema string) mcpclient.Tool {
	return mcpclient.Tool{Name: "test-tool", InputSchema: json.RawMessage(schema)}
}

func TestParamFieldsSortedAndRequiredFlagged(t *testing.T) {
	schema, err := parseSchema(json.RawMessage(`{
		"type": "object",
		"properties": {
			"zebra": {"type": "string"},
			"apple": {"type": "integer", "description": "count"},
			"banana": {"type": "boolean"}
		},
		"required": ["apple"]
	}`))
	if err != nil {
		t.Fatalf("parseSchema() error = %v", err)
	}

	fields := paramFields(schema)
	if len(fields) != 3 {
		t.Fatalf("paramFields() = %v, want 3 fields", fields)
	}
	wantOrder := []string{"apple", "banana", "zebra"}
	for i, name := range wantOrder {
		if fields[i].Name != name {
			t.Errorf("fields[%d].Name = %q, want %q (alphabetical order)", i, fields[i].Name, name)
		}
	}
	if !fields[0].Required {
		t.Errorf("apple.Required = false, want true")
	}
	if fields[1].Required || fields[2].Required {
		t.Errorf("banana/zebra should not be required")
	}
	if fields[0].Description != "count" {
		t.Errorf("apple.Description = %q, want %q", fields[0].Description, "count")
	}
}

func TestParseSchemaEmpty(t *testing.T) {
	schema, err := parseSchema(nil)
	if err != nil {
		t.Fatalf("parseSchema(nil) error = %v", err)
	}
	if len(paramFields(schema)) != 0 {
		t.Errorf("paramFields() on empty schema = %v, want none", paramFields(schema))
	}
}

func TestParseSchemaInvalidJSON(t *testing.T) {
	if _, err := parseSchema(json.RawMessage(`{not json`)); err == nil {
		t.Error("parseSchema() on invalid JSON, want an error")
	}
}

func TestNewCallFormNoParameters(t *testing.T) {
	cf, err := newCallForm(mcpclient.Tool{Name: "test-tool"})
	if err != nil {
		t.Fatalf("newCallForm() error = %v", err)
	}
	args, err := cf.Arguments()
	if err != nil {
		t.Fatalf("Arguments() error = %v", err)
	}
	if len(args) != 0 {
		t.Errorf("Arguments() = %v, want empty map", args)
	}
}

// TestCallFormArgumentsCoercion exercises Arguments()'s type coercion
// directly against the pointers newCallForm binds each huh field to --
// equivalent to what the field would hold after a user filled it in,
// without needing to drive huh's own interactive Update loop.
func TestCallFormArgumentsCoercion(t *testing.T) {
	cf, err := newCallForm(testTool(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"count": {"type": "integer"},
			"ratio": {"type": "number"},
			"enabled": {"type": "boolean"},
			"tags": {"type": "array"},
			"empty_optional": {"type": "string"}
		},
		"required": ["name"]
	}`))
	if err != nil {
		t.Fatalf("newCallForm() error = %v", err)
	}

	*cf.strs["name"] = "PROJ-1"
	*cf.strs["count"] = "3"
	*cf.strs["ratio"] = "1.5"
	*cf.bools["enabled"] = true
	*cf.strs["tags"] = `["a", "b"]`
	// empty_optional left as "" -- should be omitted entirely.

	args, err := cf.Arguments()
	if err != nil {
		t.Fatalf("Arguments() error = %v", err)
	}

	if args["name"] != "PROJ-1" {
		t.Errorf("args[name] = %v, want %q", args["name"], "PROJ-1")
	}
	if args["count"] != int64(3) {
		t.Errorf("args[count] = %v (%T), want int64(3)", args["count"], args["count"])
	}
	if args["ratio"] != 1.5 {
		t.Errorf("args[ratio] = %v, want 1.5", args["ratio"])
	}
	if args["enabled"] != true {
		t.Errorf("args[enabled] = %v, want true", args["enabled"])
	}
	tags, ok := args["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("args[tags] = %v, want a 2-element slice", args["tags"])
	}
	if _, present := args["empty_optional"]; present {
		t.Errorf("args[empty_optional] should be omitted, got %v", args["empty_optional"])
	}
}

func TestCallFormArgumentsRejectsInvalidNumber(t *testing.T) {
	cf, err := newCallForm(testTool(`{"type": "object", "properties": {"count": {"type": "integer"}}}`))
	if err != nil {
		t.Fatalf("newCallForm() error = %v", err)
	}
	*cf.strs["count"] = "not-a-number"

	if _, err := cf.Arguments(); err == nil {
		t.Error("Arguments() with an invalid integer, want an error")
	}
}

func TestCallFormArgumentsRejectsInvalidJSON(t *testing.T) {
	cf, err := newCallForm(testTool(`{"type": "object", "properties": {"data": {"type": "object"}}}`))
	if err != nil {
		t.Fatalf("newCallForm() error = %v", err)
	}
	*cf.strs["data"] = `{not valid json`

	if _, err := cf.Arguments(); err == nil {
		t.Error("Arguments() with invalid JSON, want an error")
	}
}

func TestNewCallFormEnumBecomesSelect(t *testing.T) {
	cf, err := newCallForm(testTool(`{
		"type": "object",
		"properties": {
			"level": {"type": "string", "enum": ["low", "high"]}
		}
	}`))
	if err != nil {
		t.Fatalf("newCallForm() error = %v", err)
	}
	if _, ok := cf.strs["level"]; !ok {
		t.Fatal("expected an enum field to still be bound as a string value")
	}
	*cf.strs["level"] = "high"
	args, err := cf.Arguments()
	if err != nil {
		t.Fatalf("Arguments() error = %v", err)
	}
	if args["level"] != "high" {
		t.Errorf("args[level] = %v, want %q", args["level"], "high")
	}
}
