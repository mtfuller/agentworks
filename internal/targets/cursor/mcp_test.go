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
	wantPath := filepath.Join(outDir, ".cursor", "mcp.json")
	if dest != wantPath {
		t.Errorf("exportMCP() dest = %q, want %q", dest, wantPath)
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
	// The server's files are shipped under .cursor/mcp-servers/<name>/ and the
	// command locates them through Cursor's ${workspaceFolder}.
	want := "cd '${workspaceFolder}/.cursor/mcp-servers/jira-fetch' && python3 src/main.py"
	if server.Command != "sh" || len(server.Args) != 2 || server.Args[1] != want {
		t.Errorf("server = %+v, want sh -c %q", server, want)
	}
	if server.Env["JIRA_API_TOKEN"] != "${JIRA_API_TOKEN}" {
		t.Errorf("server.Env[JIRA_API_TOKEN] = %q, want ${JIRA_API_TOKEN}", server.Env["JIRA_API_TOKEN"])
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

func TestExportMCPMergesServersIntoOneFile(t *testing.T) {
	outDir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		a, err := scaffold.New(t.TempDir(), artifact.KindMCP, name, scaffold.Options{Description: "x"})
		if err != nil {
			t.Fatalf("scaffold.New() error = %v", err)
		}
		if _, err := exportMCP(a, outDir); err != nil {
			t.Fatalf("exportMCP(%s) error = %v", name, err)
		}
	}
	// Re-exporting an existing server replaces its entry rather than duplicating it.
	again, _ := scaffold.New(t.TempDir(), artifact.KindMCP, "one", scaffold.Options{Description: "x"})
	if _, err := exportMCP(again, outDir); err != nil {
		t.Fatalf("re-export error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc mcpconfig.File
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.MCPServers) != 2 {
		t.Errorf("mcp.json has %d servers, want both one and two: %s", len(doc.MCPServers), data)
	}
}
