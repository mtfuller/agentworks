package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCanonicalizesAndPromotesConfirmation(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistry()
	readonly, err := registry.Bind(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if readonly.Confirmed || !IsAdHoc(readonly.Alias) {
		t.Fatalf("binding = %#v", readonly)
	}
	confirmed, err := registry.Bind(filepath.Join(root, "."), true)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Alias != readonly.Alias || !confirmed.Confirmed {
		t.Fatalf("confirmed = %#v, readonly = %#v", confirmed, readonly)
	}
	resolved, ok := registry.Resolve(readonly.Alias)
	if !ok || !resolved.Confirmed || resolved.Path != confirmed.Path {
		t.Fatalf("resolved = %#v, ok=%v", resolved, ok)
	}
}

func TestRegistryRejectsMissingAndNonDirectoryPaths(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Bind("", true); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := registry.Bind(filepath.Join(t.TempDir(), "missing"), true); err == nil {
		t.Fatal("missing path accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Bind(file, true); err == nil {
		t.Fatal("file path accepted")
	}
}

func TestSnapshotReportsPortableChangesAndSkipsGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "keep.txt"), "before")
	writeTestFile(t, filepath.Join(root, "delete.txt"), "delete")
	writeTestFile(t, filepath.Join(root, ".git", "ignored"), "before")
	before, err := Capture(root)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "keep.txt"), "after")
	writeTestFile(t, filepath.Join(root, "new", "added.txt"), "added")
	writeTestFile(t, filepath.Join(root, ".git", "ignored"), "after")
	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(root)
	if err != nil {
		t.Fatal(err)
	}
	changes := Compare(before, after)
	if len(changes.Added) != 1 || changes.Added[0] != "new/added.txt" ||
		len(changes.Modified) != 1 || changes.Modified[0] != "keep.txt" ||
		len(changes.Deleted) != 1 || changes.Deleted[0] != "delete.txt" || changes.Incomplete {
		t.Fatalf("changes = %#v", changes)
	}
	for _, path := range append(append(changes.Added, changes.Modified...), changes.Deleted...) {
		if filepath.IsAbs(path) {
			t.Fatalf("absolute path exposed: %q", path)
		}
	}
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
