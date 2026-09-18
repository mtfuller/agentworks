package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestExportSkill(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "csv-analyzer", scaffold.Options{Description: "Analyze a CSV and flag rows that stand out."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportSkill(a, outDir)
	if err != nil {
		t.Fatalf("exportSkill() error = %v", err)
	}
	wantPath := filepath.Join(outDir, ".cursor", "rules", "csv-analyzer.mdc")
	if dest != wantPath {
		t.Errorf("exportSkill() dest = %q, want %q", dest, wantPath)
	}

	content, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	if !strings.Contains(string(content), "description: Analyze a CSV and flag rows that stand out.") {
		t.Errorf("%s missing description field, got:\n%s", wantPath, content)
	}
	if !strings.Contains(string(content), "alwaysApply: false") {
		t.Errorf("%s missing alwaysApply: false, got:\n%s", wantPath, content)
	}

	// The skill's own scaffolded supporting files should be copied
	// alongside the rule, not just the manifest translated.
	filesDir := filepath.Join(outDir, ".cursor", "rules", "csv-analyzer")
	if _, err := os.Stat(filesDir); err != nil {
		t.Errorf("expected %s to exist with the skill's supporting files: %v", filesDir, err)
	}
}
