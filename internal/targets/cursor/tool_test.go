package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
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
	wantPath := filepath.Join(outDir, ".cursor", "mcp.json")
	if dest != wantPath {
		t.Errorf("exportTool() dest = %q, want %q", dest, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	var doc mcpconfig.File
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing mcp.json: %v", err)
	}
	server, ok := doc.MCPServers["jira-fetch"]
	if !ok {
		t.Fatalf("mcp.json missing server entry, got: %+v", doc)
	}
	if server.Command != "sh" || len(server.Args) != 2 || server.Args[1] != "python3 src/main.py" {
		t.Errorf("server = %+v, want sh -c \"python3 src/main.py\"", server)
	}
	if server.Env["JIRA_API_TOKEN"] != "${JIRA_API_TOKEN}" {
		t.Errorf("server.Env[JIRA_API_TOKEN] = %q, want ${JIRA_API_TOKEN}", server.Env["JIRA_API_TOKEN"])
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
