package geminicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestExportTool(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "jira-fetch", scaffold.Options{Description: "Fetch a Jira issue."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["command"] = "python3 src/main.py"
	a.Extra["auth"] = []string{"JIRA_API_TOKEN"}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindMCP)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportMCP(reloaded, outDir)
	if err != nil {
		t.Fatalf("exportMCP() error = %v", err)
	}
	wantDir := filepath.Join(outDir, "jira-fetch")
	if dest != wantDir {
		t.Errorf("exportMCP() dest = %q, want %q", dest, wantDir)
	}

	data, err := os.ReadFile(filepath.Join(wantDir, "gemini-extension.json"))
	if err != nil {
		t.Fatalf("reading gemini-extension.json: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing gemini-extension.json: %v", err)
	}
	server, ok := m.MCPServers["jira-fetch"]
	if !ok {
		t.Fatalf("manifest.MCPServers missing entry, got: %+v", m.MCPServers)
	}
	if server.Command != "sh" || len(server.Args) != 2 || server.Args[1] != "python3 src/main.py" {
		t.Errorf("server = %+v, want sh -c \"python3 src/main.py\"", server)
	}
	if server.Env["JIRA_API_TOKEN"] != "${JIRA_API_TOKEN}" {
		t.Errorf("server.Env[JIRA_API_TOKEN] = %q, want ${JIRA_API_TOKEN}", server.Env["JIRA_API_TOKEN"])
	}

	if _, err := os.Stat(filepath.Join(wantDir, "GEMINI.md")); err == nil {
		t.Error("expected no GEMINI.md for a bare tool export")
	}
}

func TestExportMCPRequiresCommand(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "no-command", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["command"] = "" // the default scaffold is runnable; simulate an author who cleared it
	if _, err := exportMCP(a, t.TempDir()); err == nil {
		t.Fatal("exportMCP() with no command expected error, got nil")
	}
}

func TestExportMCPRemoteUsesGeminiKeys(t *testing.T) {
	tests := []struct {
		transport string
		wantKey   string
		absentKey string
	}{
		{"http", "httpUrl", "url"},
		{"sse", "url", "httpUrl"},
	}
	for _, tt := range tests {
		a, err := scaffold.New(t.TempDir(), artifact.KindMCP, "remote", scaffold.Options{Description: "x"})
		if err != nil {
			t.Fatalf("scaffold.New() error = %v", err)
		}
		a.Extra = map[string]any{"transport": tt.transport, "url": "https://example.com/mcp"}

		outDir := t.TempDir()
		if _, err := exportMCP(a, outDir); err != nil {
			t.Fatalf("%s: exportMCP() error = %v", tt.transport, err)
		}
		data, err := os.ReadFile(filepath.Join(outDir, "remote", "gemini-extension.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		entry := doc.MCPServers["remote"]
		if entry[tt.wantKey] != "https://example.com/mcp" {
			t.Errorf("%s: want %q set, got %v", tt.transport, tt.wantKey, entry)
		}
		if _, has := entry[tt.absentKey]; has {
			t.Errorf("%s: %q should not be set, got %v", tt.transport, tt.absentKey, entry)
		}
		if _, has := entry["type"]; has {
			t.Errorf("%s: Gemini has no \"type\" field, got %v", tt.transport, entry)
		}
	}
}
