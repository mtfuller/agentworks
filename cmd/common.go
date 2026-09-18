package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/project"
)

// projectRoot resolves the AgentWorks project root from the global
// --project flag, walking upward the same way git finds a repo root.
func projectRoot() (string, error) {
	root, err := project.FindRoot(projectFlag)
	if err != nil {
		return "", fmt.Errorf("%w (run 'agentworks init' first, or pass --project)", err)
	}
	return root, nil
}

// loadArtifactAtPath loads the artifact rooted at path, which may either be
// an artifact's directory (e.g. "skills/demo") or a direct path to its
// <kind>.md manifest.
func loadArtifactAtPath(path string) (*artifact.Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".md") {
			return nil, fmt.Errorf("%s: expected an artifact directory or a <kind>.md file", path)
		}
		return artifact.LoadFile(path)
	}

	for _, k := range artifact.Kinds() {
		manifest := path + string(os.PathSeparator) + k.FileName()
		if _, err := os.Stat(manifest); err == nil {
			return artifact.Load(path, k)
		}
	}
	return nil, fmt.Errorf("%s: no agent.md/skill.md/mcp.md/hook.md found", path)
}

// confirmProceed asks a plain y/N question on stdin, defaulting to "no" on
// anything but an explicit "y"/"yes" (including a read error/EOF) -- used
// as an interactive confirmation gate before a command writes something a
// security-relevant scan flagged (see LintSecurity/cmd/add.go).
func confirmProceed(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// hashArtifactDirs computes one combined content hash over one or more
// artifact directories (more than one for a bundle export), each hashed
// individually with lockfile.HashDir and folded together sorted by dir so
// the result doesn't depend on argument order -- shared by cmd/export.go
// (recording what a source looked like at export time) and cmd/status.go
// (recomputing it later to check for staleness).
func hashArtifactDirs(dirs []string) (string, error) {
	sorted := append([]string(nil), dirs...)
	sort.Strings(sorted)

	h := sha256.New()
	for _, dir := range sorted {
		dh, err := lockfile.HashDir(dir)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%s\n", dir, dh)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// securityGate scans artifacts for LintSecurity warnings (a hook/mcp
// command that will run arbitrary shell code with the user's own
// permissions) and enforces the write-time confirmation gate shared by
// `agentworks add` and `agentworks update --apply`: print every warning,
// then only proceed if yes is set (the caller's --yes flag), or -- in an
// interactive terminal -- the user explicitly confirms. Returns false
// (never erroring) when the user declines interactively; returns an error
// when running non-interactively without --yes, since silently proceeding
// without any confirmation at all would defeat the point of the gate.
func securityGate(sourceDesc string, artifacts []*artifact.Artifact, yes bool) (bool, error) {
	var warnings []artifact.LintWarning
	for _, a := range artifacts {
		warnings = append(warnings, a.LintSecurity()...)
	}
	if len(warnings) == 0 {
		return true, nil
	}

	color.Warning("%s declares content that will run shell commands with your permissions:", sourceDesc)
	for _, w := range warnings {
		color.Warning("  %s: %s", w.Dir, w.Message)
	}
	if yes {
		return true, nil
	}
	if isInteractive() {
		return confirmProceed("Proceed anyway?"), nil
	}
	return false, fmt.Errorf("refusing to write without confirmation -- review the warnings above and re-run with --yes")
}

// importerSource converts a lockfile.SourceRef back into an
// importer.Source, used by `agentworks update` to re-Fetch a locked import
// from exactly the source it was pinned to.
func importerSource(ref lockfile.SourceRef) importer.Source {
	return importer.Source{
		Kind: importer.SourceKind(ref.Type),
		Repo: ref.Repo,
		Ref:  ref.Ref,
		Path: ref.Path,
		URL:  ref.URL,
	}
}
