package geminicli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestExportAgent(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Digs into a topic and reports back."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["tools"] = []string{"read-files", "web-search"}
	a.Extra["model"] = "balanced"
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindAgent)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportAgent(reloaded, outDir)
	if err != nil {
		t.Fatalf("exportAgent() error = %v", err)
	}
	wantPath := filepath.Join(outDir, ".gemini", "agents", "researcher.md")
	if dest != wantPath {
		t.Errorf("exportAgent() dest = %q, want %q", dest, wantPath)
	}

	content, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	for _, want := range []string{"name: researcher", "model: gemini-flash-latest", "read_file", "web_search"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("%s missing %q, got:\n%s", wantPath, want, content)
		}
	}
}

func TestExportAgentOmitsUnsetToolsAndModel(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "plain", scaffold.Options{Description: "No capabilities set."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportAgent(a, outDir)
	if err != nil {
		t.Fatalf("exportAgent() error = %v", err)
	}
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading %s: %v", dest, err)
	}
	// Checked against the frontmatter block only: the scaffolded body text
	// below it legitimately mentions "tools:"/"model:" in prose.
	frontmatter := strings.SplitN(string(content), "\n---\n", 2)[0]
	if strings.Contains(frontmatter, "tools:") || strings.Contains(frontmatter, "model:") {
		t.Errorf("%s frontmatter unexpectedly contains a tools:/model: field, got:\n%s", dest, frontmatter)
	}
}
