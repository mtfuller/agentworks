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
			if err := smokeTestMCP(a); err != nil {
				t.Errorf("smokeTestMCP() error = %v", err)
			}
		})
	}
}
