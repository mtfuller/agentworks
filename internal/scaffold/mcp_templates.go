package scaffold

import (
	"sort"

	"github.com/mtfuller/agentworks/internal/artifact"
)

const pythonMCPTestCommand = `python3 -m unittest discover -s tests -p "test_*.py"`

// mcpBodyTmpl is the shared markdown body for a locally-run mcp scaffold.
// entry is the file that owns the protocol loop; run is the command that
// starts it.
func mcpBodyTmpl(entry, run string) string {
	return `# {{.Title}}

{{.Description}}

## Tools

List each tool this server exposes to an agent: its name, what it does, and
its arguments. Tools are registered in ` + entry + `; each one's name,
description, and JSON Schema are what the agent sees.

## Developing

- ` + "`agentworks run <path>`" + ` starts the server (` + "`" + run + "`" + `) and opens an
  inspector: browse its tools, fill in arguments, and see the raw JSON-RPC.
- ` + "`agentworks test <path>`" + ` runs the tests declared in ` + "`test:`" + `; without one it
  falls back to a smoke check (start the server, run the handshake, list tools).
- ` + "`agentworks doctor <path>`" + ` checks the interpreter and any ` + "`auth`" + ` environment
  variables are available.

## Configuration

` + "`command`" + ` (with optional ` + "`args`" + `) is what starts the server. ` + "`auth`" + ` lists
required environment variables and ` + "`env`" + ` sets non-secret ones; on export, ` + "`auth`" + `
entries are passed through as ` + "`${VAR}`" + ` references, never literal secrets.
` + "`agentworks export`" + ` turns these fields into each target's MCP registration.
`
}

// mcpTemplates are the built-in templates for the mcp kind. They're appended
// to templates (and re-sorted into kind order) in init below.
var mcpTemplates = []Template{
	{
		ID: "api-wrapper", Kind: artifact.KindMCP,
		Title:       "API wrapper",
		Description: "Exposes a REST API's endpoints to an agent as MCP tools.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/server.py",
					"command":    "python3 src/server.py",
					"test":       pythonMCPTestCommand,
					"auth":       []string{"API_BASE_URL", "API_TOKEN"},
				}
			},
			bodyTmpl: mcpBodyTmpl("`src/server.py`", "python3 src/server.py"),
			files: func(name string) []extraFile {
				return []extraFile{
					{"src/server.py", pythonMCPServerWithTools(name, pythonAPITools)},
					{"tests/test_server.py", mcpSource(pythonMCPTestGeneric, name)},
				}
			},
			extraDirs: []string{"src", "tests"},
		},
	},
	{
		ID: "cli-wrapper", Kind: artifact.KindMCP,
		Title:       "CLI wrapper",
		Description: "Exposes a local command-line tool to an agent as an MCP tool.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/server.py",
					"command":    "python3 src/server.py",
					"test":       pythonMCPTestCommand,
					"auth":       []string{},
				}
			},
			bodyTmpl: mcpBodyTmpl("`src/server.py`", "python3 src/server.py"),
			files: func(name string) []extraFile {
				return []extraFile{
					{"src/server.py", pythonMCPServerWithTools(name, pythonCLITools)},
					{"tests/test_server.py", mcpSource(pythonMCPTestGeneric, name)},
				}
			},
			extraDirs: []string{"src", "tests"},
		},
	},
	{
		ID: "node-mcp", Kind: artifact.KindMCP,
		Title:       "Node.js MCP server",
		Description: "A dependency-free Node.js MCP server, for tools too involved for a quick script.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/index.js",
					"command":    "node src/index.js",
					"test":       "npm test",
					"auth":       []string{},
				}
			},
			bodyTmpl: mcpBodyTmpl("`src/index.js`", "node src/index.js"),
			files: func(name string) []extraFile {
				return []extraFile{
					{"package.json", nodePackageJSON(name, "src/index.js")},
					{"src/index.js", mcpSource(nodeMCPServer, name)},
					{"tests/index.test.js", mcpSource(nodeMCPTest, name)},
				}
			},
			extraDirs: []string{"src", "tests"},
		},
	},
	{
		ID: "node-ts-mcp", Kind: artifact.KindMCP,
		Title:       "TypeScript MCP server",
		Description: "A TypeScript MCP server, type-checked with tsc and bundled with esbuild into a self-contained output.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/index.ts",
					"command":    "node dist/index.js",
					"build":      "npm install && npx tsc --noEmit && npx esbuild src/index.ts --bundle --platform=node --format=esm --outfile=dist/index.js",
					"test":       "npm test",
					"auth":       []string{},
				}
			},
			bodyTmpl: mcpBodyTmpl("`src/index.ts`", "node dist/index.js") + `
## Building

` + "`build:`" + ` installs dependencies, type-checks, and bundles to ` + "`dist/index.js`" + `, which
` + "`command`" + ` runs. Run ` + "`agentworks build <path>`" + ` before ` + "`run`" + ` or ` + "`export`" + ` (export does
it for you). Bundling also inlines any shared local package -- see README.md,
"Sharing code between Node artifacts".
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"package.json", nodeTSPackageJSON(name, "src/index.ts", "dist/index.js")},
					{"tsconfig.json", nodeTSConfig()},
					{"src/index.ts", mcpSource(tsMCPServer, name)},
					{"tests/index.test.ts", mcpSource(tsMCPTest, name)},
				}
			},
			extraDirs: []string{"src", "tests"},
		},
	},
	{
		ID: "npx-wrapper", Kind: artifact.KindMCP,
		Title:       "npx package",
		Description: "Registers a published MCP server package run through npx -- no code to write.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"command": "npx",
					"args":    []string{"-y", "REPLACE-WITH-PACKAGE-NAME"},
					"auth":    []string{},
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

Replace ` + "`REPLACE-WITH-PACKAGE-NAME`" + ` in ` + "`args`" + ` with the package that publishes the
server (pin a version, e.g. ` + "`some-mcp-server@1.2.3`" + `, so exports are reproducible).
List any required secrets under ` + "`auth`" + ` -- they're passed through as ` + "`${VAR}`" + `
references on export -- and non-secret settings under ` + "`env`" + `.

` + "`agentworks run <path>`" + ` starts the package locally to inspect its tools.
`,
		},
	},
	{
		ID: "remote-http", Kind: artifact.KindMCP,
		Title:       "Remote HTTP server",
		Description: "Registers a hosted MCP server by URL -- no local process or code.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"transport": "http",
					"url":       "https://example.com/mcp",
					"headers":   map[string]string{"Authorization": "Bearer ${API_TOKEN}"},
					"auth":      []string{"API_TOKEN"},
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

Set ` + "`url`" + ` to the server's endpoint. Use ` + "`transport: sse`" + ` for a legacy
server-sent-events endpoint. Reference secrets in ` + "`headers`" + ` as ` + "`${VAR}`" + ` and
list each variable under ` + "`auth`" + `; ` + "`agentworks validate`" + ` rejects a literal
credential in a sensitive header so none is written into exported config.

Remote servers have no local files, so ` + "`agentworks run`" + ` (which starts a local
process) doesn't apply to them.
`,
		},
	},
}

func init() {
	templates = append(templates, mcpTemplates...)
	order := map[artifact.Kind]int{}
	for i, k := range artifact.Kinds() {
		order[k] = i
	}
	sort.SliceStable(templates, func(i, j int) bool { return order[templates[i].Kind] < order[templates[j].Kind] })
}
