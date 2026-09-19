package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Each command's --json document, keyed by the `command` value it reports.
// docs/schemas/<name>.schema.json is generated from these Go types, so the
// published schema can't disagree with what the command actually emits.
var jsonDocTypes = map[string]any{
	"list":        listDoc{},
	"validate":    validateDoc{},
	"doctor":      doctorDoc{},
	"test":        testDoc{},
	"eval":        evalDoc{},
	"status":      statusDoc{},
	"targets":     targetsDoc{},
	"marketplace": marketplaceCheckDoc{},
	"version":     versionDoc{},
	"add":         addDoc{},
	"update":      updateDoc{},
	"export":      exportDoc{},
	"build":       buildDoc{},
	"error":       errorDoc{},
}

const schemasDir = "../docs/schemas"

func TestPublishedJSONSchemasAreCurrent(t *testing.T) {
	update := os.Getenv("UPDATE_GOLDEN") != ""
	if update {
		if err := os.MkdirAll(schemasDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for name, doc := range jsonDocTypes {
		want := renderSchema(t, name, doc)
		path := filepath.Join(schemasDir, name+".schema.json")

		if update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v (generate with UPDATE_GOLDEN=1 go test ./cmd)", path, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date with the %s document type -- regenerate with: UPDATE_GOLDEN=1 go test ./cmd\n"+
				"(if a field was removed or retyped, that is a breaking change: bump jsonSchemaVersion)", path, name)
		}
	}
}

func renderSchema(t *testing.T, name string, doc any) []byte {
	t.Helper()
	schema := schemaOf(reflect.TypeOf(doc))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "https://github.com/mtfuller/agentworks/docs/schemas/" + name + ".schema.json"
	schema["title"] = "agentworks " + name + " --json"

	props := schema["properties"].(map[string]any)
	if name != "error" {
		props["command"] = map[string]any{"type": "string", "const": name}
	}
	props["schema_version"] = map[string]any{"type": "integer", "const": jsonSchemaVersion}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// schemaOf derives a JSON Schema from a Go type, following encoding/json's
// rules: `json` tags name properties, `omitempty` makes one optional,
// embedded structs are flattened. Objects allow additional properties, since
// consumers are told to ignore keys they don't know.
func schemaOf(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		collectFields(t, props, &required)
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	return map[string]any{}
}

func collectFields(t reflect.Type, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" {
			collectFields(f.Type, props, required)
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		props[name] = schemaOf(f.Type)
		if !strings.Contains(opts, "omitempty") {
			*required = append(*required, name)
		}
	}
}
