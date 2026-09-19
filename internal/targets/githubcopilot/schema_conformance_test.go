package githubcopilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

// These tests hold the Copilot exporter to the real Agent Plugins schemas
// (vendored under ../testdata/agentplugins). The schemas are closed -- no
// unknown fields -- and strict about names and transports, and Copilot CLI
// itself accepted an mcp.json that violated them, so a passing install proves
// little: the schema is the check.

const schemaBase = "https://agent-plugins.org/schemas/1.0.0/"

func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "testdata", "agentplugins", name+".schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parsing the %s schema: %v", name, err)
	}
	rewriteLookahead(doc)
	c := jsonschema.NewCompiler()
	url := schemaBase + name + ".schema.json"
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compiling the %s schema: %v", name, err)
	}
	return sch
}

// nameLookahead is the plugin schema's name pattern prefix. Go's regexp (RE2)
// has no lookahead, so the loader rewrites `^(?!.*(?:--|\.\.))X$` -- "X, and
// contains neither -- nor .." -- into the equivalent `^X$` plus a `not`
// constraint. Nothing else about the schema is changed.
const nameLookahead = `^(?!.*(?:--|\.\.))`

func rewriteLookahead(doc any) {
	root, ok := doc.(map[string]any)
	if !ok {
		return
	}
	props, _ := root["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	pattern, _ := name["pattern"].(string)
	if rest, found := strings.CutPrefix(pattern, nameLookahead); found {
		name["pattern"] = "^" + rest
		name["not"] = map[string]any{"pattern": `--|\.\.`}
	}
}

// validateFile validates a JSON file against a compiled schema.
func validateFile(t *testing.T, sch *jsonschema.Schema, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Errorf("expected %s: %v", path, err)
		return
	}
	defer f.Close()
	inst, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Errorf("%s is not JSON: %v", path, err)
		return
	}
	if err := sch.Validate(inst); err != nil {
		t.Errorf("%s violates the Agent Plugins schema:\n%v", path, err)
	}
}

// conformanceArtifacts builds one of everything the exporter handles, covering
// each MCP transport.
func conformanceArtifacts(t *testing.T) []*artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	mk := func(kind artifact.Kind, name string, opts scaffold.Options, extra map[string]any) *artifact.Artifact {
		t.Helper()
		if opts.Description == "" {
			opts.Description = "A " + name + " artifact used to check the exported plugin against the schema."
		}
		a, err := scaffold.New(root, kind, name, opts)
		if err != nil {
			t.Fatalf("scaffold.New(%s) error = %v", name, err)
		}
		for k, v := range extra {
			a.Extra[k] = v
		}
		return a
	}

	remoteHTTP := mk(artifact.KindMCP, "remote-http", scaffold.Options{Template: "remote-http"}, nil)
	remoteSSE := mk(artifact.KindMCP, "remote-sse", scaffold.Options{Template: "remote-http"}, map[string]any{"transport": "sse", "url": "https://example.com/sse"})

	return []*artifact.Artifact{
		mk(artifact.KindSkill, "csv-analyzer", scaffold.Options{}, nil),
		mk(artifact.KindAgent, "researcher", scaffold.Options{}, nil),
		mk(artifact.KindHook, "guard", scaffold.Options{}, map[string]any{
			"handlers": []any{map[string]any{"event": "preToolUse", "matcher": "bash", "command": "echo checking", "timeout": 10}},
		}),
		mk(artifact.KindMCP, "greeter", scaffold.Options{}, nil),
		mk(artifact.KindMCP, "with-args", scaffold.Options{}, map[string]any{
			"command": "npx", "args": []any{"-y", "some-server@1.2.3"}, "env": map[string]any{"REGION": "us-east-1"}, "auth": []any{"TOKEN"},
		}),
		remoteHTTP,
		remoteSSE,
	}
}

func TestEveryExportConformsToTheAgentPluginsSchemas(t *testing.T) {
	pluginSchema := compileSchema(t, "plugin")
	mcpSchema := compileSchema(t, "mcp")
	meta := targets.PluginMeta{
		Version: "1.2.3", License: "MIT", Homepage: "https://example.com", Repository: "https://github.com/o/r",
		Author: targets.Author{Name: "Ada", Email: "ada@example.com", URL: "https://example.com/ada"},
	}
	arts := conformanceArtifacts(t)

	// Each artifact on its own.
	for _, a := range arts {
		a := a
		t.Run("standalone/"+a.Name, func(t *testing.T) {
			dest, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{Meta: meta})
			if err != nil {
				t.Fatalf("Export() error = %v", err)
			}
			validateFile(t, pluginSchema, filepath.Join(dest, "plugin.json"))
			if a.Kind == artifact.KindMCP {
				validateFile(t, mcpSchema, filepath.Join(dest, "mcp.json"))
			}
		})
	}

	// All of them as one bundle, under a project name the schema would reject as is.
	t.Run("bundle", func(t *testing.T) {
		dest, err := (exporter{}).ExportBundle("My Project_1", "Everything.", arts, t.TempDir(), targets.ExportOptions{Meta: meta})
		if err != nil {
			t.Fatalf("ExportBundle() error = %v", err)
		}
		if filepath.Base(dest) != "my-project-1" {
			t.Errorf("bundle directory = %q, want the slugged plugin name", filepath.Base(dest))
		}
		validateFile(t, pluginSchema, filepath.Join(dest, "plugin.json"))
		validateFile(t, mcpSchema, filepath.Join(dest, "mcp.json"))
	})

	// With no publisher details at all, the output is still valid.
	t.Run("bundle without metadata", func(t *testing.T) {
		dest, err := (exporter{}).ExportBundle("kit", "d", arts, t.TempDir(), targets.ExportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		validateFile(t, pluginSchema, filepath.Join(dest, "plugin.json"))
	})
}

func TestRemoteServersUseTheAgentPluginsTransportNames(t *testing.T) {
	arts := conformanceArtifacts(t)
	dest, err := (exporter{}).ExportBundle("kit", "d", arts, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"type": "streamable-http"`) || !strings.Contains(text, `"type": "sse"`) {
		t.Errorf("mcp.json should name the transports the schema does (streamable-http, sse):\n%s", text)
	}
	if strings.Contains(text, `"type": "http"`) {
		t.Errorf("\"http\" is Claude Code's name for streamable HTTP, not Agent Plugins':\n%s", text)
	}
}

// The check above is only worth something if the validator can fail. This
// pins the two defects the schema found in earlier exports, so a regression
// (or a vendored schema that stopped being strict) is noticed.
func TestTheSchemasRejectWhatEarlierExportsWrote(t *testing.T) {
	mcpSchema := compileSchema(t, "mcp")
	pluginSchema := compileSchema(t, "plugin")
	check := func(sch *jsonschema.Schema, doc string) error {
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		return sch.Validate(inst)
	}

	if check(mcpSchema, `{"$schema":"`+schemaBase+`mcp.schema.json","mcpServers":{"r":{"type":"http","url":"https://x.example/mcp"}}}`) == nil {
		t.Error(`the schema should reject type "http" for a remote server`)
	}
	if check(pluginSchema, `{"$schema":"`+schemaBase+`plugin.schema.json","name":"My Project"}`) == nil {
		t.Error("the schema should reject a plugin name with a space and capitals")
	}
	if check(pluginSchema, `{"$schema":"`+schemaBase+`plugin.schema.json","name":"ok","hooks":{}}`) == nil {
		t.Error("the plugin schema is closed: an unknown field must be rejected")
	}
}
