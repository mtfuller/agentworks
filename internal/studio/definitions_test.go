package studio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDefinitionKinds(t *testing.T) {
	tests := []struct {
		path    string
		kind    string
		content string
		wantErr bool
	}{
		{"agentworks.yaml", "project", "format: 2\nname: example\n", false},
		{"agentworks.yaml", "project", "format: 99\nname: example\n", true},
		{"teams/engineering/team.yaml", "team", "name: engineering\nagents: [reviewer]\n", false},
		{"teams/engineering/team.yaml", "team", "name: other\nagents: [reviewer]\n", true},
		{"agents/reviewer/AGENT.md", "agent", "---\nname: reviewer\n---\nReview.\n", false},
		{"agents/reviewer/AGENT.md", "agent", "missing frontmatter", true},
		{"tools/jira/tool.yaml", "tool", "name: jira\ntransport: stdio\nruntime:\n  variants:\n    - provider: host\n      command: echo\n", false},
		{"tools/jira/tool.yaml", "tool", "name: wrong\ntransport: stdio\nruntime:\n  variants: []\n", true},
		{"skills/testing/SKILL.md", "skill", "---\nname: testing\ndescription: Test changed behavior carefully.\n---\nTest it.\n", false},
		{"skills/testing/SKILL.md", "skill", "---\nname: wrong\ndescription: Test changed behavior carefully.\n---\nTest it.\n", true},
		{"memory/team.md", "memory", "# Anything\n", false},
		{"sources/nightly/source.yaml", "source", "name: nightly\nkind: schedule\nschedule:\n  every: 1h\nevent:\n  type: maintenance.requested\n", false},
		{"routes/work/route.yaml", "route", "name: work\nwhen:\n  type: work.requested\ninvoke:\n  team: engineering\n  workspace: product\n", false},
		{"other/file.md", "other", "text", true},
	}
	for _, test := range tests {
		t.Run(test.kind+"/"+filepath.Base(filepath.Dir(test.path)), func(t *testing.T) {
			err := validateDefinition(test.path, test.kind, []byte(test.content))
			if (err != nil) != test.wantErr {
				t.Fatalf("validateDefinition() error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestDefinitionKindAllowlist(t *testing.T) {
	tests := map[string]string{
		"agentworks.yaml": "project", "teams/a/team.yaml": "team", "agents/a/AGENT.md": "agent",
		"skills/a/SKILL.md": "skill", "tools/a/tool.yaml": "tool", "memory/team.md": "memory",
		"sources/a/source.yaml": "source", "routes/a/route.yaml": "route",
		"agentworks.local.yaml": "", ".agentworks/deps/skills/a/SKILL.md": "", "teams/a/other.yaml": "",
	}
	for path, want := range tests {
		if got := definitionKind(path); got != want {
			t.Errorf("definitionKind(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDefinitionEditorErrorResponsesAndSymlinkExclusion(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/definitions", "/api/v1/definitions/agentworks.yaml"} {
		request := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s status=%d", path, response.Code)
		}
	}
	cookie, csrf := studioSession(t, handler)
	request := httptest.NewRequest(http.MethodPut, "http://localhost/api/v1/definitions/agentworks.yaml", strings.NewReader(`{"expected_sha256":"x","content":"format: 2"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("rootless PUT status = %d", response.Code)
	}

	root, runtimeStore := manualRunFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "memory", "linked.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	handler, err = NewHandlerWithConfig(HandlerConfig{ProjectName: "example", ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/definitions/memory/linked.md", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("symlink status = %d body=%s", response.Code, response.Body.String())
	}

	cookie, csrf = studioSession(t, handler)
	notFound := putDefinition(t, handler, cookie, csrf, "memory/missing.md", "x", "text")
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("missing PUT status = %d body=%s", notFound.Code, notFound.Body.String())
	}
	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{"malformed", `{`, http.StatusBadRequest},
		{"missing hash", `{"content":"format: 2"}`, http.StatusBadRequest},
		{"trailing", `{"expected_sha256":"x","content":"x"} {}`, http.StatusBadRequest},
		{"unknown field", `{"expected_sha256":"x","content":"x","extra":true}`, http.StatusBadRequest},
		{"nul", `{"expected_sha256":"x","content":"a\u0000b"}`, http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "http://localhost/api/v1/definitions/agentworks.yaml", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-AgentWorks-CSRF", csrf)
			request.Header.Set("Origin", "http://localhost")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
