package geminicli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// manifest is a gemini-extension.json manifest (see
// github.com/google-gemini/gemini-cli/blob/main/docs/extensions/reference.md).
// Only the fields AgentWorks has a real value for are populated; the rest
// (contextFileName, excludeTools, settings, themes, ...) are left for a
// project to hand-edit if it needs them.
type manifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Description string            `json:"description,omitempty"`
	MCPServers  map[string]server `json:"mcpServers,omitempty"`
}

// server is Gemini CLI's mcpServers entry. It differs from the shared
// mcpconfig.Server for remote servers: Gemini keys streamable-HTTP off
// "httpUrl" and SSE off "url", and has no "type" discriminator.
type server struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	HTTPURL string            `json:"httpUrl,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Cwd is where a stdio server runs. Gemini's default working directory for
	// an extension's server is undocumented, so a server that ships files sets
	// it to ${extensionPath}, which Gemini substitutes with the extension's
	// directory.
	Cwd string `json:"cwd,omitempty"`
}

func serverFrom(s mcpconfig.Server, remote bool) server {
	out := server{Command: s.Command, Args: s.Args, Env: s.Env, Headers: s.Headers}
	if !remote && s.Command != "" {
		out.Cwd = "${extensionPath}"
	}
	switch s.Type {
	case mcpconfig.TransportHTTP:
		out.HTTPURL = s.URL
	case mcpconfig.TransportSSE:
		out.URL = s.URL
	}
	return out
}

// writeManifest writes extDir/gemini-extension.json.
func writeManifest(extDir string, m manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding gemini-extension.json: %w", err)
	}
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", extDir, err)
	}
	path := filepath.Join(extDir, "gemini-extension.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
