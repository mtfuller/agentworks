package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// exportTool writes outDir/.cursor/mcp.json: Cursor's project-level MCP
// config (.cursor/mcp.json) is confirmed identical in shape to Claude
// Desktop's claude_desktop_config.json ({"mcpServers": {"name": {command,
// args, env}}}) -- the same shape internal/targets/mcpconfig already builds
// for claudecode/githubcopilot.
func exportTool(a *artifact.Artifact, outDir string) (string, error) {
	server, err := mcpconfig.ServerFor(a)
	if err != nil {
		return "", err
	}

	doc := mcpconfig.File{MCPServers: map[string]mcpconfig.Server{a.Name: server}}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding mcp.json: %w", err)
	}

	dir := filepath.Join(outDir, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
