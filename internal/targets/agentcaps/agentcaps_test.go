package agentcaps

import "testing"

func TestIsValidTool(t *testing.T) {
	for _, tool := range ValidTools() {
		if !IsValidTool(tool) {
			t.Errorf("IsValidTool(%q) = false, want true", tool)
		}
	}
	if IsValidTool("nonsense") {
		t.Error("IsValidTool(\"nonsense\") = true, want false")
	}
}

func TestIsValidModel(t *testing.T) {
	for _, m := range ValidModels() {
		if !IsValidModel(m) {
			t.Errorf("IsValidModel(%q) = false, want true", m)
		}
	}
	if IsValidModel("nonsense") {
		t.Error("IsValidModel(\"nonsense\") = true, want false")
	}
}

func TestForClaudeCode(t *testing.T) {
	tests := []struct {
		name      string
		tools     []string
		model     string
		wantTools string
		wantModel string
	}{
		{"empty input omits both fields", nil, "", "", ""},
		{"read-files maps to read-only tools", []string{ReadFiles}, "", "Glob, Grep, Read", ""},
		{"edit-files maps to write tools", []string{EditFiles}, "", "Edit, Write", ""},
		{"run-commands and code-execution both map to Bash, deduped", []string{RunCommands, CodeExecution}, "", "Bash", ""},
		{"web-search maps to WebSearch and WebFetch", []string{WebSearch}, "", "WebFetch, WebSearch", ""},
		{"unrecognized tool contributes nothing", []string{"nonsense"}, "", "", ""},
		{"model tiers map to Claude Code aliases", nil, ModelFast, "", "haiku"},
		{"balanced tier maps to sonnet", nil, ModelBalanced, "", "sonnet"},
		{"powerful tier maps to opus", nil, ModelPowerful, "", "opus"},
		{"unrecognized model contributes nothing", nil, "nonsense", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTools, gotModel := ForClaudeCode(tt.tools, tt.model)
			if gotTools != tt.wantTools {
				t.Errorf("ForClaudeCode(%v, %q) tools = %q, want %q", tt.tools, tt.model, gotTools, tt.wantTools)
			}
			if gotModel != tt.wantModel {
				t.Errorf("ForClaudeCode(%v, %q) model = %q, want %q", tt.tools, tt.model, gotModel, tt.wantModel)
			}
		})
	}
}

func TestForGeminiCLI(t *testing.T) {
	tests := []struct {
		name      string
		tools     []string
		model     string
		wantTools []string
		wantModel string
	}{
		{"empty input omits both fields", nil, "", nil, ""},
		{"read-files maps to read-only tools", []string{ReadFiles}, "", []string{"glob", "grep_search", "read_file"}, ""},
		{"edit-files maps to write tools", []string{EditFiles}, "", []string{"replace", "write_file"}, ""},
		{"run-commands and code-execution both map to run_shell_command, deduped", []string{RunCommands, CodeExecution}, "", []string{"run_shell_command"}, ""},
		{"web-search maps to web_search and web_fetch", []string{WebSearch}, "", []string{"web_fetch", "web_search"}, ""},
		{"unrecognized tool contributes nothing", []string{"nonsense"}, "", nil, ""},
		{"fast tier maps to gemini-flash-lite-latest", nil, ModelFast, nil, "gemini-flash-lite-latest"},
		{"balanced tier maps to gemini-flash-latest", nil, ModelBalanced, nil, "gemini-flash-latest"},
		{"powerful tier maps to gemini-pro-latest", nil, ModelPowerful, nil, "gemini-pro-latest"},
		{"unrecognized model contributes nothing", nil, "nonsense", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTools, gotModel := ForGeminiCLI(tt.tools, tt.model)
			if len(gotTools) != len(tt.wantTools) {
				t.Fatalf("ForGeminiCLI(%v, %q) tools = %v, want %v", tt.tools, tt.model, gotTools, tt.wantTools)
			}
			for i := range gotTools {
				if gotTools[i] != tt.wantTools[i] {
					t.Errorf("ForGeminiCLI(%v, %q) tools = %v, want %v", tt.tools, tt.model, gotTools, tt.wantTools)
				}
			}
			if gotModel != tt.wantModel {
				t.Errorf("ForGeminiCLI(%v, %q) model = %q, want %q", tt.tools, tt.model, gotModel, tt.wantModel)
			}
		})
	}
}
