package export

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/lockfile"
)

func TestClearMergedFilesOnlyAfterAPriorExport(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"mcpServers":{"gone":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	lf := &lockfile.Lockfile{Exports: map[string]lockfile.ExportEntry{}}
	clearMergedFiles(lf, "cursor", dir)
	if _, err := os.Stat(file); err != nil {
		t.Fatal("a file AgentWorks never exported must be left alone")
	}

	lf.Exports["cursor:mcp/x"] = lockfile.ExportEntry{Target: "cursor"}
	clearMergedFiles(lf, "gemini-cli", dir)
	if _, err := os.Stat(file); err != nil {
		t.Fatal("another target's export must not clear this target's files")
	}
	clearMergedFiles(lf, "cursor", dir)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("stale merged file should be cleared after a prior export")
	}
}
