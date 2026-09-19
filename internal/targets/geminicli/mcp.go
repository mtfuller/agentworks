package geminicli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// exportMCP writes a Gemini CLI extension directory at outDir/<name>/ with
// only gemini-extension.json's "mcpServers" field populated -- confirmed to
// support the same server shape (command/args/env) internal/targets/mcpconfig
// already builds for claudecode/githubcopilot/cursor, just nested one level
// under the manifest instead of written as a bare top-level file. No
// GEMINI.md is needed for a bare tool with no context to add. A local
// server's own files are copied into the extension, since an extension is a
// self-contained installable directory (Gemini CLI copies it on install).
func exportMCP(a *artifact.Artifact, outDir string) (string, error) {
	srv, err := mcpconfig.ServerFor(a)
	if err != nil {
		return "", err
	}

	extDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(extDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", extDir, err)
	}

	m := manifest{
		Name:        a.Name,
		Version:     a.Version,
		Description: a.Description,
		MCPServers:  map[string]server{a.Name: serverFrom(srv, mcpconfig.IsRemote(a))},
	}
	if err := writeManifest(extDir, m); err != nil {
		return "", err
	}
	// A local server's files are part of the extension, and it runs from there.
	if !mcpconfig.IsRemote(a) {
		if err := filecopy.CopyArtifactFiles(a, extDir); err != nil {
			return "", err
		}
	}
	return extDir, nil
}
