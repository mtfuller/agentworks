package cursor

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
	outDir := t.TempDir()

	dest, err := exportAgent(a, outDir)
	if err != nil {
		t.Fatalf("exportAgent() error = %v", err)
	}
	wantPath := filepath.Join(outDir, ".cursor", "agents", "researcher.md")
	if dest != wantPath {
		t.Errorf("exportAgent() dest = %q, want %q", dest, wantPath)
	}

	content, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	if !strings.Contains(string(content), "name: researcher") {
		t.Errorf("%s missing name field, got:\n%s", wantPath, content)
	}
	if !strings.Contains(string(content), "description: Digs into a topic and reports back.") {
		t.Errorf("%s missing description field, got:\n%s", wantPath, content)
	}
	// No tools:/model: fields yet -- see agent.go's doc comment for why.
	// (Checked against the frontmatter block only: the scaffolded body
	// text below it legitimately mentions "tools:"/"model:" in prose.)
	frontmatter := strings.SplitN(string(content), "\n---\n", 2)[0]
	if strings.Contains(frontmatter, "tools:") || strings.Contains(frontmatter, "model:") {
		t.Errorf("%s frontmatter unexpectedly contains a tools:/model: field, got:\n%s", wantPath, frontmatter)
	}
}
