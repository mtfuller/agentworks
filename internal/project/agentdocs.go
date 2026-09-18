package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// agentsMDTemplate is the AGENTS.md written into every new project by Init.
// It orients a coding agent working in this specific project (not
// AgentWorks' own repo) and points it at the CLI skill for command detail.
const agentsMDTemplate = `# AGENTS.md

Guidance for coding agents working in this project.

## What this project is

**%s** is an [AgentWorks](https://github.com/mtfuller/agentworks) project: agents,
skills, tools, hooks, and workflows are authored here as plain files, then exported to
whatever vendor format a given AI harness needs (Claude Code, ChatGPT, GitHub Copilot,
Microsoft 365 Copilot, and others) with the ` + "`agentworks`" + ` CLI.

## Layout

` + "```" + `
agentworks.yaml   project manifest: name, description, default targets, publisher
agents/           agent definitions (agent.md + resources/)
skills/           skills (skill.md + scripts/tests/samples)
tools/            MCP-server-backed tools (tool.md + src/tests)
hooks/            lifecycle hooks (hook.md)
workflows/        multi-artifact pipelines (workflow.md)
` + "```" + `

Each artifact is a directory containing one ` + "`<kind>.md`" + ` file (YAML frontmatter plus a
Markdown body) alongside whatever supporting files it needs. It's plain text — read and
edit it directly rather than going through the CLI for inspection.

## Working with this project

See [.agents/skills/agentworks-cli/SKILL.md](.agents/skills/agentworks-cli/SKILL.md) for
how to scaffold, validate, test, and export artifacts with the ` + "`agentworks`" + ` CLI. Prefer
the CLI over hand-writing ` + "`<kind>.md`" + ` files from scratch (` + "`agentworks new`" + `) and over
hand-editing exported vendor output (` + "`agentworks export`" + ` regenerates it).
` + "`agentworks validate`" + ` also warns on weak descriptions (too vague, too long, or
indistinguishable from another artifact's) -- worth heeding even though it won't fail
the command unless ` + "`--strict`" + ` is passed, since a bad description is how an agent picks
the wrong artifact or misses this one entirely.
`

// agentworksCLISkillTemplate is the SKILL.md written into every new project
// at .agents/skills/agentworks-cli/, teaching a coding agent how to drive
// the agentworks CLI for this specific project.
const agentworksCLISkillTemplate = `---
name: agentworks-cli
description: >
  How to scaffold, validate, test, and export this project's agents, skills, tools,
  hooks, and workflows with the agentworks CLI. Use whenever asked to add, change,
  check, or export an artifact in this project.
---

# Using the agentworks CLI

This project is managed by [AgentWorks](https://github.com/mtfuller/agentworks). Prefer
these commands over hand-writing or hand-editing files under ` + "`agents/`" + `, ` + "`skills/`" + `,
` + "`tools/`" + `, ` + "`hooks/`" + `, or ` + "`workflows/`" + ` directly.

## Commands

- ` + "`agentworks new <kind> [name] --description \"...\"`" + ` — scaffold a new artifact
  (` + "`kind`" + ` is one of ` + "`agent`" + `/` + "`skill`" + `/` + "`tool`" + `/` + "`hook`" + `/` + "`workflow`" + `). Leave out
  ` + "`name`" + `/` + "`description`" + ` in an interactive terminal to get a short wizard instead.
  Pass ` + "`--from-template <id>`" + ` to start from a curated built-in template — see
  ` + "`agentworks templates [kind]`" + ` for the list.
- ` + "`agentworks list [kind]`" + ` — table of this project's discovered artifacts.
- ` + "`agentworks validate [path]`" + ` — parse and validate one artifact, or the whole
  project if no path is given. Run this after hand-editing any ` + "`<kind>.md`" + ` file.
  Beyond structural checks, it also warns on weak descriptions (too short, too long, or
  overlapping heavily with another artifact's) -- pass ` + "`--strict`" + ` to fail on those too.
- ` + "`agentworks build [path]`" + ` — run the ` + "`build:`" + ` command an artifact declares in
  its frontmatter (installing dependencies, compiling, bundling, or whatever else it
  needs before it can run or be exported; no path runs every artifact that declares
  one).
- ` + "`agentworks test [path]`" + ` — run the ` + "`test:`" + ` command an artifact declares in its
  frontmatter (no path runs every artifact that declares one).
- ` + "`agentworks targets`" + ` — capability matrix of which vendor targets support which
  artifact kinds, and whether a real exporter exists for that combination.
- ` + "`agentworks export <path> --target <id>`" + ` — export one artifact to a vendor's
  native format (e.g. ` + "`--target claude-code`" + `, ` + "`--target github-copilot`" + `,
  ` + "`--target chatgpt`" + `, ` + "`--target m365-copilot`" + `). Use ` + "`--all`" + ` or ` + "`--kind <kind>`" + `
  instead of a path to export the whole project (or one kind of it) in one call. Pass
  several paths, or one path with ` + "`--bundle <name>`" + `, to package multiple artifacts
  into a single plugin (` + "`claude-code`" + `/` + "`github-copilot`" + ` only).
- ` + "`agentworks add <url>`" + ` — import a published Agent Skill or Claude Code plugin
  into this project (the reverse of export). Accepts a GitHub ` + "`owner/repo`" + `
  shorthand, a repo/tree/blob URL, or a direct archive URL. With no argument in an
  interactive terminal, opens the marketplace search TUI instead.
- ` + "`agentworks marketplace`" + ` — publish this whole project as a plugin marketplace
  repo: exports every artifact into committed ` + "`plugins/`" + ` directories (bundled one
  per namespace, or ` + "`--single`" + ` for one plugin total) and writes
  ` + "`.claude-plugin/marketplace.json`" + ` / ` + "`.github/plugin/marketplace.json`" + ` so a
  team can add this repo directly as a plugin source instead of installing artifacts
  one at a time.
- ` + "`agentworks tui`" + ` — full-screen browser for all of the above: drill into an
  artifact, then ` + "`n`" + `ew/` + "`e`" + `xport/` + "`t`" + `est/` + "`a`" + `dd/` + "`b`" + `rowse-templates without
  dropping back to individual CLI calls.

Global flags: ` + "`-p, --project`" + ` (path inside the project to operate on, default
` + "`.`" + `, resolved upward like ` + "`git`" + ` finds a repo root), ` + "`-v, --verbose`" + `,
` + "`-l, --log-level`" + `.

## Workflow for a typical change

1. ` + "`agentworks new <kind> <name> --description \"...\"`" + ` to scaffold, or edit an
   existing artifact's ` + "`<kind>.md`" + ` / supporting files directly.
2. ` + "`agentworks validate`" + ` to catch frontmatter and cross-reference problems
   (a workflow's ` + "`steps:`" + ` resolving to real artifacts, a hook's
   ` + "`events`" + `/` + "`command`" + ` set together, a tool's ` + "`auth`" + ` requiring ` + "`command`" + `).
3. ` + "`agentworks build <path>`" + ` if the artifact declares a ` + "`build:`" + ` command.
4. ` + "`agentworks test <path>`" + ` if the artifact declares a ` + "`test:`" + ` command.
5. ` + "`agentworks export <path> --target <id>`" + ` for each vendor this artifact needs
   to ship to. Never hand-edit the exported output — re-export instead.
`

// writeAgentDocs writes AGENTS.md and the .agents/skills/agentworks-cli/
// SKILL.md into a freshly-initialized project, so a coding agent working in
// it immediately knows how to drive the agentworks CLI. It does not
// overwrite either file if already present, so re-running init-adjacent
// tooling never clobbers hand edits.
func writeAgentDocs(dir, name string) error {
	agentsMDPath := filepath.Join(dir, "AGENTS.md")
	if err := writeIfAbsent(agentsMDPath, fmt.Sprintf(agentsMDTemplate, name)); err != nil {
		return err
	}

	skillDir := filepath.Join(dir, ".agents", "skills", "agentworks-cli")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", skillDir, err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := writeIfAbsent(skillPath, agentworksCLISkillTemplate); err != nil {
		return err
	}
	return nil
}

func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
