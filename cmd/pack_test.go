package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPackCreateAndInstallCommands(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "examples", "agent-team"))
	archive := filepath.Join(t.TempDir(), "engineering.agentworks")
	created := mustRun(t, root, "pack", "engineering", "--output", archive, "--json")
	var createDoc struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
		Result struct {
			Files int `json:"files"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(created.stdout), &createDoc); err != nil {
		t.Fatalf("decode create output: %v\n%s", err, created.stdout)
	}
	if !createDoc.OK || createDoc.Action != "create" || createDoc.Result.Files == 0 {
		t.Fatalf("create doc=%#v", createDoc)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "installed")
	installed := runCLIRaw(t, "pack", "install", archive, destination, "--json")
	if installed.err != nil {
		t.Fatalf("install failed: %v\n%s", installed.err, installed.combined())
	}
	var installDoc struct {
		OK          bool   `json:"ok"`
		Action      string `json:"action"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal([]byte(installed.stdout), &installDoc); err != nil {
		t.Fatalf("decode install output: %v\n%s", err, installed.stdout)
	}
	if !installDoc.OK || installDoc.Action != "install" || installDoc.Destination != destination {
		t.Fatalf("install doc=%#v", installDoc)
	}
	if _, err := os.Stat(filepath.Join(destination, "agentworks.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestExportResolvedFormat2Team(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "examples", "agent-team"))
	out := t.TempDir()
	result := mustRun(t, root, "export", "--team", "engineering", "--target", "claude-code", "--out", out, "--json")
	var document struct {
		OK      bool `json:"ok"`
		Outputs []struct {
			Target string `json:"target"`
			Path   string `json:"path"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &document); err != nil {
		t.Fatalf("decode export: %v\n%s", err, result.stdout)
	}
	if !document.OK || len(document.Outputs) != 1 || document.Outputs[0].Target != "claude-code" {
		t.Fatalf("export=%#v", document)
	}
	if _, err := os.Stat(filepath.Join(out, "claude-code", "engineering", ".agentworks-plan.json")); err != nil {
		t.Fatal(err)
	}
}
