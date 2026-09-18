// Package agentcaps is AgentWorks' vendor-agnostic vocabulary for what an
// agent may do and how capable a model it needs: a small, closed set of
// "tools" capabilities and "model" tiers set via an agent artifact's
// frontmatter, plus the per-vendor mapping functions that turn them into
// each target's real, native shape (Claude Code's comma-separated `tools:`
// allowlist + `model:` alias, Gemini CLI's `tools:` array + `model:` alias).
// GitHub Copilot's custom-agent frontmatter has no publicly confirmed
// tools/model fields yet, so there's deliberately no ForGitHubCopilot here
// -- see internal/targets/githubcopilot/agent.go. Cursor's subagent
// frontmatter is in the same position (no `tools:` field, no stable model
// alias) -- see internal/targets/cursor/agent.go.
//
// Tiers and curated tool categories are used instead of literal per-vendor
// tool/model names so the mapping stays valid as vendors rename or add
// models and tools -- the same reasoning already applied to the ChatGPT
// decision elsewhere in this codebase.
package agentcaps

import (
	"sort"
	"strings"
)

// Tool identifiers in AgentWorks' vendor-agnostic vocabulary, set via an
// agent artifact's "tools:" frontmatter field (a YAML list). Not every
// vendor has a real equivalent for every one of these.
const (
	ReadFiles     = "read-files"
	EditFiles     = "edit-files"
	RunCommands   = "run-commands"
	WebSearch     = "web-search"
	CodeExecution = "code-execution"
)

// Model tiers, set via an agent artifact's "model:" frontmatter field (a
// single string). Portable stand-ins for a literal model ID.
const (
	ModelFast     = "fast"
	ModelBalanced = "balanced"
	ModelPowerful = "powerful"
)

// ValidTools returns every recognized "tools:" value, in a stable order.
func ValidTools() []string {
	return []string{ReadFiles, EditFiles, RunCommands, WebSearch, CodeExecution}
}

// IsValidTool reports whether s is a recognized "tools:" value.
func IsValidTool(s string) bool {
	for _, t := range ValidTools() {
		if s == t {
			return true
		}
	}
	return false
}

// ValidModels returns every recognized "model:" value, in a stable order.
func ValidModels() []string {
	return []string{ModelFast, ModelBalanced, ModelPowerful}
}

// IsValidModel reports whether s is a recognized "model:" value.
func IsValidModel(s string) bool {
	for _, m := range ValidModels() {
		if s == m {
			return true
		}
	}
	return false
}

// ForClaudeCode maps AgentWorks' vendor-agnostic tools/model to Claude
// Code's real subagent frontmatter shape: a comma-separated "tools:"
// allowlist and a "model:" alias (see
// https://code.claude.com/docs/en/sub-agents). Empty/unrecognized input on
// either side yields an empty return value so the caller can omit the
// field entirely -- Claude Code's own default when "tools:" is omitted is
// to inherit every tool available to subagents, and omitting "model:"
// resolves it from the main conversation, both of which match AgentWorks'
// own default (an agent scaffolded without "tools:"/"model:" set behaves
// exactly as it did before this mapping existed).
func ForClaudeCode(tools []string, model string) (toolsField, modelField string) {
	claudeTools := map[string]bool{}
	for _, t := range tools {
		switch t {
		case ReadFiles:
			claudeTools["Read"] = true
			claudeTools["Glob"] = true
			claudeTools["Grep"] = true
		case EditFiles:
			claudeTools["Edit"] = true
			claudeTools["Write"] = true
		case RunCommands, CodeExecution:
			claudeTools["Bash"] = true
		case WebSearch:
			claudeTools["WebSearch"] = true
			claudeTools["WebFetch"] = true
		}
	}
	if len(claudeTools) > 0 {
		names := make([]string, 0, len(claudeTools))
		for name := range claudeTools {
			names = append(names, name)
		}
		sort.Strings(names)
		toolsField = strings.Join(names, ", ")
	}

	switch model {
	case ModelFast:
		modelField = "haiku"
	case ModelBalanced:
		modelField = "sonnet"
	case ModelPowerful:
		modelField = "opus"
	}
	return toolsField, modelField
}

// ForGeminiCLI maps AgentWorks' vendor-agnostic tools/model to Gemini CLI's
// real subagent frontmatter shape: a "tools:" array of Gemini CLI's actual
// built-in tool names (see
// https://geminicli.com/docs/core/subagents/) and a "model:" field. Unlike
// Cursor (see internal/targets/cursor/agent.go), Gemini CLI documents both
// a real tools array and real evergreen model aliases
// ("gemini-flash-lite-latest"/"gemini-flash-latest"/"gemini-pro-latest" --
// Google's own stable, deliberately hot-swapped pointers, the same kind of
// tier alias ForClaudeCode already uses for "haiku"/"sonnet"/"opus"), so
// there's something honest to map to. Empty/unrecognized input on either
// side yields an empty return value, matching ForClaudeCode's own "omit the
// field, let the vendor's default apply" behavior.
func ForGeminiCLI(tools []string, model string) (toolsField []string, modelField string) {
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			toolsField = append(toolsField, name)
		}
	}
	for _, t := range tools {
		switch t {
		case ReadFiles:
			add("read_file")
			add("glob")
			add("grep_search")
		case EditFiles:
			add("write_file")
			add("replace")
		case RunCommands, CodeExecution:
			add("run_shell_command")
		case WebSearch:
			add("web_search")
			add("web_fetch")
		}
	}
	sort.Strings(toolsField)

	switch model {
	case ModelFast:
		modelField = "gemini-flash-lite-latest"
	case ModelBalanced:
		modelField = "gemini-flash-latest"
	case ModelPowerful:
		modelField = "gemini-pro-latest"
	}
	return toolsField, modelField
}
