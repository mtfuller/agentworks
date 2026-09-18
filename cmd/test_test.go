package cmd

import (
	"os/exec"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// Every mcp scaffold must validate as generated, and every locally-run one
// must actually speak MCP: the built-in smoke test (the same code path as
// `agentworks test` on an mcp artifact with no test:) starts the scaffolded
// server, runs the handshake, and lists its tools. Needs the scaffold's
// runtime on PATH, so each case skips when it isn't there.
func TestScaffoldedMCPServersAreWorking(t *testing.T) {
	cases := []struct {
		template string // "" = the default scaffold
		runtime  string // binary the server needs; "" = none (not run locally)
		smoke    bool
	}{
		{"", "python3", true},
		{"api-wrapper", "python3", false}, // needs API_BASE_URL/API_TOKEN, so the smoke test skips itself
		{"cli-wrapper", "python3", true},
		{"node-mcp", "node", true},
		{"node-ts-mcp", "", false}, // needs npm install + build before it runs
		{"npx-wrapper", "", false}, // placeholder package
		{"remote-http", "", false},
	}
	for _, tc := range cases {
		name := tc.template
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			opts := scaffold.Options{Description: "A server used to check the scaffold works."}
			if tc.template != "" {
				opts.Template = tc.template
			}
			a, err := scaffold.New(t.TempDir(), artifact.KindMCP, "demo", opts)
			if err != nil {
				t.Fatalf("scaffold.New() error = %v", err)
			}
			if err := validateKindSpecific(a); err != nil {
				t.Fatalf("scaffold doesn't validate out of the box: %v", err)
			}
			if _, err := mcpconfig.ServerFor(a); err != nil {
				t.Fatalf("scaffold doesn't export out of the box: %v", err)
			}

			if !tc.smoke {
				return
			}
			if _, err := exec.LookPath(tc.runtime); err != nil {
				t.Skipf("%s not on PATH", tc.runtime)
			}
			if !smokeEligible(a) {
				t.Fatal("smokeEligible() = false for a runnable local server")
			}
			skipped, err := smokeTestMCP(a)
			if err != nil {
				t.Errorf("smokeTestMCP() error = %v", err)
			}
			if skipped != "" {
				t.Errorf("smokeTestMCP() skipped (%s), want it to run", skipped)
			}
		})
	}
}

// Every scaffold -- each kind's default and every built-in template -- must
// use only supported frontmatter fields with the right types, so a fresh
// project passes `validate --strict` without touching anything.
func TestEveryScaffoldUsesOnlyRegisteredFields(t *testing.T) {
	type combo struct {
		kind     artifact.Kind
		template string
	}
	var combos []combo
	for _, k := range artifact.Kinds() {
		combos = append(combos, combo{k, ""})
	}
	for _, tmpl := range scaffold.Templates() {
		combos = append(combos, combo{tmpl.Kind, tmpl.ID})
	}

	for _, c := range combos {
		name := string(c.kind) + "/" + c.template
		t.Run(name, func(t *testing.T) {
			a, err := scaffold.New(t.TempDir(), c.kind, "demo", scaffold.Options{
				Description: "A scaffold used to check its frontmatter uses only supported fields.",
				Template:    c.template,
			})
			if err != nil {
				t.Fatalf("scaffold.New() error = %v", err)
			}
			// Reload so the check sees what YAML produces, not in-memory values.
			reloaded, err := artifact.Load(a.Dir, c.kind)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if err := reloaded.ValidateFieldTypes(); err != nil {
				t.Errorf("wrong field type: %v", err)
			}
			for _, w := range reloaded.LintFields() {
				t.Errorf("scaffold frontmatter: %s", w.Message)
			}
		})
	}
}
