package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
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

// fakeRemoteMCP serves a minimal streamable-HTTP MCP server that requires a
// bearer token and offers one tool, one resource, and one prompt.
func fakeRemoteMCP(t *testing.T, token string) *httptest.Server {
	t.Helper()
	reply := func(w http.ResponseWriter, id *int64, result any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			reply(w, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "remote-fake", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{}},
			})
		case "tools/list":
			reply(w, req.ID, map[string]any{"tools": []map[string]any{{"name": "echo"}}})
		case "resources/list":
			reply(w, req.ID, map[string]any{"resources": []map[string]any{{"uri": "mem://a", "name": "a"}}})
		case "prompts/list":
			reply(w, req.ID, map[string]any{"prompts": []map[string]any{{"name": "greet"}}})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func remoteArtifact(t *testing.T, url string) *artifact.Artifact {
	t.Helper()
	a, err := scaffold.New(t.TempDir(), artifact.KindMCP, "remote", scaffold.Options{Description: "A remote server used to check the smoke test.", Template: "remote-http"})
	if err != nil {
		t.Fatal(err)
	}
	a.Extra["url"] = url
	return a
}

func TestSmokeTestRemoteMCPServer(t *testing.T) {
	srv := fakeRemoteMCP(t, "s3cret")
	a := remoteArtifact(t, srv.URL)

	// Off by default: a remote smoke test makes network calls.
	testRemote = false
	if smokeEligible(a) {
		t.Error("a remote server must not be smoke-tested unless --remote is passed")
	}
	testRemote = true
	t.Cleanup(func() { testRemote = false })
	if !smokeEligible(a) {
		t.Fatal("with --remote a remote server should be smoke-tested")
	}

	// Without the variable the template's header references, it is skipped, not failed.
	t.Setenv("API_TOKEN", "")
	if skipped, err := smokeTestMCP(a); err != nil || skipped == "" {
		t.Errorf("smokeTestMCP() without API_TOKEN = (%q, %v), want a skip", skipped, err)
	}

	t.Setenv("API_TOKEN", "s3cret")
	if skipped, err := smokeTestMCP(a); err != nil || skipped != "" {
		t.Errorf("smokeTestMCP() = (%q, %v), want it to pass", skipped, err)
	}

	t.Setenv("API_TOKEN", "wrong")
	if _, err := smokeTestMCP(a); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("smokeTestMCP() with a bad token error = %v, want a 401", err)
	}
}

func TestMCPTargetForLocalAndRemote(t *testing.T) {
	local, err := scaffold.New(t.TempDir(), artifact.KindMCP, "local", scaffold.Options{Description: "A local server used to check its target."})
	if err != nil {
		t.Fatal(err)
	}
	target, missing := mcpTarget(local)
	if target.IsRemote() || target.Command != "python3 src/server.py" || target.Dir != local.Dir || len(missing) != 0 {
		t.Errorf("local target = %+v, missing %v", target, missing)
	}

	remote := remoteArtifact(t, "https://example.com/mcp")
	t.Setenv("API_TOKEN", "abc")
	target, missing = mcpTarget(remote)
	if !target.IsRemote() || target.Transport != "http" || target.URL != "https://example.com/mcp" {
		t.Errorf("remote target = %+v", target)
	}
	if target.Headers["Authorization"] != "Bearer abc" || len(missing) != 0 {
		t.Errorf("headers = %v, missing %v; want ${API_TOKEN} expanded from the environment", target.Headers, missing)
	}

	t.Setenv("API_TOKEN", "")
	target, missing = mcpTarget(remote)
	if _, sent := target.Headers["Authorization"]; sent {
		t.Error("a header referencing an unset variable must be left out, not sent half-expanded")
	}
	if len(missing) != 1 || missing[0] != "API_TOKEN" {
		t.Errorf("missing = %v, want [API_TOKEN]", missing)
	}
}

func TestCheckRunnableAllowsRemoteServers(t *testing.T) {
	if err := checkRunnable(remoteArtifact(t, "https://example.com/mcp")); err != nil {
		t.Errorf("checkRunnable() on a remote server error = %v, want it allowed", err)
	}
	noURL := remoteArtifact(t, "")
	delete(noURL.Extra, "url")
	if err := checkRunnable(noURL); err == nil || !strings.Contains(err.Error(), "url") {
		t.Errorf("checkRunnable() on a remote server with no url error = %v, want it to ask for one", err)
	}
}
