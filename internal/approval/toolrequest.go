package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mtfuller/agentworks/internal/store"
)

// ToolRequest is the small vendor-neutral payload accepted by the run-scoped
// permission gateway. Vendor helpers discard session IDs and other metadata
// before constructing it.
type ToolRequest struct {
	ToolName      string          `json:"tool_name"`
	Input         json.RawMessage `json:"input"`
	SandboxBypass bool            `json:"sandbox_bypass,omitempty"`
}

// NormalizeToolRequest converts a vendor tool call into one of AgentWorks'
// bounded approval scopes. Unknown, malformed, readonly, and sandbox-bypass
// requests fail closed.
func NormalizeToolRequest(request ToolRequest, workspace string, permission store.Permission) (Request, error) {
	if permission == store.PermissionReadonly {
		return Request{}, errors.New("readonly runs cannot approve mutating tools")
	}
	if request.SandboxBypass {
		return Request{}, errors.New("sandbox bypass cannot be approved")
	}
	name := strings.ToLower(strings.TrimSpace(request.ToolName))
	var input map[string]any
	if len(request.Input) == 0 || json.Unmarshal(request.Input, &input) != nil {
		return Request{}, errors.New("tool input must be a JSON object")
	}
	switch name {
	case "write", "edit", "multiedit", "notebookedit", "create", "apply_patch", "str_replace_editor":
		path := firstString(input, "file_path", "path", "notebook_path", "filePath")
		if path == "" {
			return Request{}, errors.New("write request does not identify a path")
		}
		directory, err := boundedDirectory(workspace, path)
		if err != nil {
			return Request{}, err
		}
		scope, _ := json.Marshal(map[string]string{"directory": directory})
		return Request{Kind: "filesystem.write", Scope: scope, Summary: "Allow edits within " + directory + " for this run"}, nil
	case "bash", "powershell", "shell", "run_shell_command":
		command := firstString(input, "command", "cmd", "script")
		if command == "" {
			return Request{}, errors.New("command request is empty")
		}
		executable, prefix := commandScope(command)
		scope, _ := json.Marshal(map[string]any{"executable": executable, "subcommand": prefix})
		return Request{Kind: "command.execute", Scope: scope, Summary: "Allow command: " + command}, nil
	default:
		return Request{}, fmt.Errorf("unsupported mutating tool %q", request.ToolName)
	}
}

func firstString(values map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := values[name].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boundedDirectory(workspace, requested string) (string, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", errors.New("resolve workspace")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("resolve workspace symlinks")
	}
	target := requested
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", errors.New("resolve requested path")
	}
	resolved, err := resolveExistingAncestor(target)
	if err != nil {
		return "", err
	}
	if !pathWithin(root, resolved) {
		return "", errors.New("write path escapes the selected workspace")
	}
	relative, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("write directory escapes the selected workspace")
	}
	if relative == "" {
		relative = "."
	}
	return filepath.ToSlash(relative), nil
}

func resolveExistingAncestor(path string) (string, error) {
	candidate := path
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("resolve requested path symlinks")
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", errors.New("requested path has no existing ancestor")
		}
		candidate = parent
	}
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		relative = strings.ToLower(relative)
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func commandScope(command string) (string, []string) {
	trimmed := strings.TrimSpace(command)
	if strings.ContainsAny(trimmed, ";&|><\n\r`$'\"") {
		return "shell", []string{trimmed}
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return "shell", []string{trimmed}
	}
	return filepath.Base(fields[0]), fields[1:]
}
