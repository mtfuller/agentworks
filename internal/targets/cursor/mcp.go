package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// exportMCP writes outDir/.cursor/mcp.json: Cursor's project-level MCP
// config (.cursor/mcp.json) is confirmed identical in shape to Claude
// Desktop's claude_desktop_config.json ({"mcpServers": {"name": {command,
// args, env}}}) -- the same shape internal/targets/mcpconfig already builds
// for claudecode/githubcopilot.
//
// Cursor has one project-level file for every server, so each export merges
// into an existing .cursor/mcp.json (replacing this server's entry by name)
// instead of overwriting it -- otherwise exporting N servers would leave only
// the last.
func exportMCP(a *artifact.Artifact, outDir string) (string, error) {
	server, err := mcpconfig.ServerFor(a)
	if err != nil {
		return "", err
	}

	path := filepath.Join(outDir, ".cursor", "mcp.json")
	doc := mcpconfig.File{MCPServers: map[string]mcpconfig.Server{}}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return "", fmt.Errorf("reading existing %s: %w", path, err)
		}
		if doc.MCPServers == nil {
			doc.MCPServers = map[string]mcpconfig.Server{}
		}
	}
	doc.MCPServers[a.Name] = server
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding mcp.json: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
