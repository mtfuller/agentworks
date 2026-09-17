// Package m365copilot implements the "m365-copilot" export target: it
// turns an AgentWorks skill or agent into a Microsoft 365 Copilot
// declarative agent, packaged as the zip "app package" Microsoft 365
// expects (an app manifest.json, a declarativeAgent.json it references,
// and placeholder icons).
//
// This is a genuinely different shape from the Agent Skills-based targets
// (see internal/targets/agentskills): a declarative agent is
// {name, description, instructions}, not a skill directory.
package m365copilot

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

func init() {
	targets.Register(agentExporter{})
}

const (
	TargetID = "m365-copilot"

	declarativeAgentSchema = "https://developer.microsoft.com/json-schemas/copilot/declarative-agent/v1.8/schema.json"
	declarativeAgentVer    = "v1.8"
	teamsManifestSchema    = "https://developer.microsoft.com/en-us/json-schemas/teams/v1.18/MicrosoftTeams.schema.json"
	teamsManifestVersion   = "1.18"
)

// declarativeAgent is the minimal declarative agent manifest (schema
// v1.8): the required version/name/description/instructions fields. See
// https://learn.microsoft.com/en-us/microsoft-365/copilot/extensibility/declarative-agent-manifest-1.7
type declarativeAgent struct {
	Schema       string `json:"$schema"`
	Version      string `json:"version"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

// teamsManifest is the (partial) Microsoft 365 app manifest -- only the
// fields required to package a declarative-agent-only app, per
// https://learn.microsoft.com/en-us/microsoft-365/copilot/extensibility/agents-are-apps
type teamsManifest struct {
	Schema          string             `json:"$schema"`
	ManifestVersion string             `json:"manifestVersion"`
	Version         string             `json:"version"`
	ID              string             `json:"id"`
	Developer       developerInfo      `json:"developer"`
	Icons           iconRefs           `json:"icons"`
	Name            localizableText    `json:"name"`
	Description     localizableText    `json:"description"`
	AccentColor     string             `json:"accentColor"`
	CopilotAgents   copilotAgentsBlock `json:"copilotAgents"`
}

type developerInfo struct {
	Name          string `json:"name"`
	WebsiteURL    string `json:"websiteUrl"`
	PrivacyURL    string `json:"privacyUrl"`
	TermsOfUseURL string `json:"termsOfUseUrl"`
}

type iconRefs struct {
	Color   string `json:"color"`
	Outline string `json:"outline"`
}

type localizableText struct {
	Short string `json:"short"`
	Full  string `json:"full"`
}

type copilotAgentsBlock struct {
	DeclarativeAgents []declarativeAgentRef `json:"declarativeAgents"`
}

type declarativeAgentRef struct {
	ID   string `json:"id"`
	File string `json:"file"`
}

type agentExporter struct{}

func (agentExporter) TargetID() string { return TargetID }

// Export ignores opts.Zip: a Microsoft 365 "app package" is a zip by
// definition (see agents-are-apps), so packaging isn't optional here the
// way it is for a skill directory.
func (agentExporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	if a.Kind != artifact.KindSkill && a.Kind != artifact.KindAgent {
		return "", fmt.Errorf("m365-copilot export supports skills and agents right now (got %s)", a.Kind)
	}
	if err := a.Validate(); err != nil {
		return "", fmt.Errorf("refusing to export invalid artifact: %w", err)
	}

	instructions := strings.TrimSpace(a.Body)
	if instructions == "" {
		instructions = a.Description
	}

	da := declarativeAgent{
		Schema:       declarativeAgentSchema,
		Version:      declarativeAgentVer,
		Name:         truncate(a.Name, 100),
		Description:  truncate(a.Description, 1000),
		Instructions: truncate(instructions, 8000),
	}

	pub, _ := publisherFor(a)
	manifest := teamsManifest{
		Schema:          teamsManifestSchema,
		ManifestVersion: teamsManifestVersion,
		Version:         "1.0.0",
		ID:              appID(a.Name),
		Developer: developerInfo{
			Name:          pub.Name,
			WebsiteURL:    pub.Website,
			PrivacyURL:    pub.PrivacyURL,
			TermsOfUseURL: pub.TermsURL,
		},
		Icons:       iconRefs{Color: "color.png", Outline: "outline.png"},
		Name:        localizableText{Short: truncate(a.Name, 30), Full: truncate(a.Name, 100)},
		Description: localizableText{Short: truncate(a.Description, 80), Full: truncate(a.Description, 4000)},
		AccentColor: pub.AccentColor,
		CopilotAgents: copilotAgentsBlock{
			DeclarativeAgents: []declarativeAgentRef{{ID: "agent1", File: "declarativeAgent.json"}},
		},
	}

	colorPNG, err := colorIcon(pub.AccentColor)
	if err != nil {
		return "", err
	}
	outlinePNG, err := outlineIcon()
	if err != nil {
		return "", err
	}

	daJSON, err := json.MarshalIndent(da, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding declarativeAgent.json: %w", err)
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding manifest.json: %w", err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", outDir, err)
	}
	zipPath := filepath.Join(outDir, a.Name+".zip")
	files := map[string][]byte{
		"manifest.json":         append(manifestJSON, '\n'),
		"declarativeAgent.json": append(daJSON, '\n'),
		"color.png":             colorPNG,
		"outline.png":           outlinePNG,
	}
	if err := writeZip(zipPath, files); err != nil {
		return "", err
	}
	return zipPath, nil
}

func writeZip(zipPath string, files map[string][]byte) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", zipPath, err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("adding %s to %s: %w", name, zipPath, err)
		}
		if _, err := w.Write(content); err != nil {
			return fmt.Errorf("writing %s in %s: %w", name, zipPath, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalizing %s: %w", zipPath, err)
	}
	return nil
}

// truncate cuts s to at most max runes, so we never emit a manifest field
// that violates the schema's length limits.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
