package m365copilot

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func readZipEntry(t *testing.T, zipPath, name string) []byte {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("opening %s: %v", zipPath, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("opening entry %s: %v", name, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("reading entry %s: %v", name, err)
			}
			return data
		}
	}
	t.Fatalf("zip %s missing entry %s", zipPath, name)
	return nil
}

func TestExportSkillProducesValidPackage(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "csv-analyzer", scaffold.Options{
		Description: "Analyze a CSV file and flag rows that stand out.",
	})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := (agentExporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "csv-analyzer.zip") {
		t.Fatalf("Export() dest = %q, want %s/csv-analyzer.zip", dest, outDir)
	}

	var da declarativeAgent
	if err := json.Unmarshal(readZipEntry(t, dest, "declarativeAgent.json"), &da); err != nil {
		t.Fatalf("parsing declarativeAgent.json: %v", err)
	}
	if da.Name != "csv-analyzer" {
		t.Errorf("declarativeAgent.Name = %q, want csv-analyzer", da.Name)
	}
	if da.Version != declarativeAgentVer {
		t.Errorf("declarativeAgent.Version = %q, want %q", da.Version, declarativeAgentVer)
	}
	if da.Instructions == "" {
		t.Error("declarativeAgent.Instructions is empty, want the artifact body")
	}

	var m teamsManifest
	if err := json.Unmarshal(readZipEntry(t, dest, "manifest.json"), &m); err != nil {
		t.Fatalf("parsing manifest.json: %v", err)
	}
	if m.ID == "" {
		t.Error("manifest.ID is empty, want a deterministic UUID")
	}
	if m.CopilotAgents.DeclarativeAgents[0].File != "declarativeAgent.json" {
		t.Errorf("copilotAgents.declarativeAgents[0].file = %q, want declarativeAgent.json", m.CopilotAgents.DeclarativeAgents[0].File)
	}
	if m.Icons.Color != "color.png" || m.Icons.Outline != "outline.png" {
		t.Errorf("Icons = %+v, want color.png/outline.png", m.Icons)
	}

	// Icons must actually be present in the package and be valid PNGs.
	for _, name := range []string{"color.png", "outline.png"} {
		data := readZipEntry(t, dest, name)
		if len(data) < 8 || string(data[1:4]) != "PNG" {
			t.Errorf("%s does not look like a PNG (len=%d)", name, len(data))
		}
	}
}

func TestAppIDIsDeterministic(t *testing.T) {
	first := appID("csv-analyzer")
	second := appID("csv-analyzer")
	if first != second {
		t.Errorf("appID() not deterministic: %q != %q", first, second)
	}
	if appID("other-name") == first {
		t.Error("appID() gave the same id for two different artifact names")
	}
	// Must be a well-formed GUID (8-4-4-4-12 hex).
	if len(first) != 36 {
		t.Errorf("appID() = %q, want a 36-character GUID", first)
	}
}

func TestExportRejectsUnsupportedKind(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := (agentExporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() of a tool expected error, got nil")
	}
}

func TestExportAgentFallsBackToDescriptionWhenBodyEmpty(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Body = "   " // whitespace-only body must not become empty `instructions`
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindAgent)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	dest, err := (agentExporter{}).Export(reloaded, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	var da declarativeAgent
	if err := json.Unmarshal(readZipEntry(t, dest, "declarativeAgent.json"), &da); err != nil {
		t.Fatalf("parsing declarativeAgent.json: %v", err)
	}
	if da.Instructions != "Researches things." {
		t.Errorf("Instructions = %q, want fallback to description", da.Instructions)
	}
}

func TestRegisteredWithTargets(t *testing.T) {
	e, err := targets.GetExporter(TargetID)
	if err != nil {
		t.Fatalf("GetExporter(%s) error = %v", TargetID, err)
	}
	if e.TargetID() != TargetID {
		t.Errorf("TargetID() = %q, want %q", e.TargetID(), TargetID)
	}
}
