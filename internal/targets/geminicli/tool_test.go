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
	a, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "Fetch a Jira issue."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["command"] = "python3 src/main.py"
	a.Extra["auth"] = []string{"JIRA_API_TOKEN"}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindTool)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportTool(reloaded, outDir)
	if err != nil {
		t.Fatalf("exportTool() error = %v", err)
	}
	wantDir := filepath.Join(outDir, "jira-fetch")
	if dest != wantDir {
		t.Errorf("exportTool() dest = %q, want %q", dest, wantDir)
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

func TestExportToolRequiresCommand(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "no-command", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := exportTool(a, t.TempDir()); err == nil {
		t.Fatal("exportTool() with no command expected error, got nil")
	}
}
