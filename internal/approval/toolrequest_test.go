package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestNormalizeToolRequestProducesBoundedDirectoryAndCommandScopes(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "src", "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		tool ToolRequest
		kind string
		want string
	}{
		{"write", ToolRequest{ToolName: "Edit", Input: json.RawMessage(`{"file_path":"src/generated/item.go"}`)}, "filesystem.write", `"directory":"src/generated"`},
		{"simple command", ToolRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"git status --short"}`)}, "command.execute", `"executable":"git"`},
		{"compound command", ToolRequest{ToolName: "bash", Input: json.RawMessage(`{"command":"go test ./... && git status"}`)}, "command.execute", `"executable":"shell"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeToolRequest(test.tool, workspace, store.PermissionReadwrite)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != test.kind || !strings.Contains(string(got.Scope), test.want) {
				t.Fatalf("request = %#v", got)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("normalized request is invalid: %v", err)
			}
		})
	}
}

func TestNormalizeToolRequestFailsClosed(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(filepath.Dir(workspace), "outside.txt")
	tests := []ToolRequest{
		{ToolName: "Edit", Input: json.RawMessage(`{"file_path":"` + filepath.ToSlash(outside) + `"}`)},
		{ToolName: "WebFetch", Input: json.RawMessage(`{"url":"https://example.com"}`)},
		{ToolName: "Bash", Input: json.RawMessage(`{"command":"go test"}`), SandboxBypass: true},
	}
	for _, request := range tests {
		if _, err := NormalizeToolRequest(request, workspace, store.PermissionReadwrite); err == nil {
			t.Fatalf("request unexpectedly accepted: %#v", request)
		}
	}
	if _, err := NormalizeToolRequest(ToolRequest{ToolName: "Edit", Input: json.RawMessage(`{"file_path":"x"}`)}, workspace, store.PermissionReadonly); err == nil {
		t.Fatal("readonly request unexpectedly accepted")
	}
}

func TestNormalizeToolRequestRejectsSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := NormalizeToolRequest(ToolRequest{ToolName: "Write", Input: json.RawMessage(`{"file_path":"link/new.txt"}`)}, workspace, store.PermissionReadwrite)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v", err)
	}
}
