package studio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeStorageUsage(t *testing.T) {
	if bytes, available, err := runtimeStorageUsage(""); err != nil || available || bytes != 0 {
		t.Fatalf("empty usage = %d, %v, %v", bytes, available, err)
	}
	root := t.TempDir()
	logRoot := filepath.Join(root, "logs")
	if err := os.MkdirAll(filepath.Join(logRoot, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.db"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logRoot, "run", "attempt.jsonl"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "state.db"), filepath.Join(logRoot, "ignored-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	bytes, available, err := runtimeStorageUsage(logRoot)
	if err != nil || !available || bytes != 10 {
		t.Fatalf("usage = %d, %v, %v; want 10, true, nil", bytes, available, err)
	}
	missing := filepath.Join(t.TempDir(), "runtime", "logs")
	if bytes, available, err := runtimeStorageUsage(missing); err != nil || !available || bytes != 0 {
		t.Fatalf("missing usage = %d, %v, %v", bytes, available, err)
	}
}
