package artifact

import (
	"fmt"
	"regexp"
)

// suspiciousCommandPatterns is a small, deliberately conservative denylist
// of shell shapes that are far more often malicious than legitimate in a
// hook/tool `command`: fetching a script and piping it straight into an
// interpreter, decoding-then-executing an obfuscated payload, a raw
// /dev/tcp reverse shell, `nc -e`, silently escalating privilege, or
// setting a setuid bit. This is a scan, not a sandbox -- it catches sloppy
// or obviously hostile commands, not a determined obfuscator, and a clean
// result is not a safety guarantee.
var suspiciousCommandPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(curl|wget)[^|]*\|\s*(sudo\s+)?(sh|bash|zsh|python[23]?|perl)\b`), "pipes a downloaded script straight into an interpreter"},
	{regexp.MustCompile(`base64\s+(-d|-D|--decode)\b`), "decodes a base64 payload, a common way to hide what a command actually runs"},
	{regexp.MustCompile(`eval\s*\$\(`), "evaluates dynamically-constructed shell code"},
	{regexp.MustCompile(`/dev/tcp/`), "opens a raw TCP socket via /dev/tcp, the classic bash reverse-shell idiom"},
	{regexp.MustCompile(`\bnc\s+.*-e\b`), "uses netcat's -e to pipe a shell to a remote connection"},
	{regexp.MustCompile(`\bchmod\s+([ugoa]*\+.*s|4[0-7]{3})\b`), "sets a setuid/setgid bit"},
	{regexp.MustCompile(`\bsudo\b`), "runs a command with elevated privileges"},
}

// LintSecurity checks a hook or tool's declared `command` for supply-chain
// risk Validate() doesn't cover: it's structurally fine (present, paired
// with events/auth as required) but will run arbitrary shell code with the
// user's own permissions the moment it's exported and triggered/invoked.
// Never fails `validate` on its own -- see cmd/validate.go's --strict flag
// -- and callers that write new artifacts to disk (`agentworks add`) use
// these as a confirmation gate rather than a silent warning.
//
// The result is the notice that a command exists (LintSecurityNotice)
// followed by any risky-shape hits (LintSecurityRisks). Callers that only
// want to fail on the latter -- `validate --strict` -- use them separately,
// since every working hook or mcp server has a command and "it has a
// command" alone isn't a defect.
func (a *Artifact) LintSecurity() []LintWarning {
	notice := a.LintSecurityNotice()
	if notice == nil {
		return nil
	}
	return append([]LintWarning{*notice}, a.LintSecurityRisks()...)
}

// LintSecurityNotice returns the informational note that this artifact runs
// a shell command with the user's permissions, or nil if it has none.
func (a *Artifact) LintSecurityNotice() *LintWarning {
	command := a.ExtraString("command")
	if command == "" {
		return nil
	}
	return &LintWarning{a.Dir, fmt.Sprintf(
		"declares a shell command that will run with your permissions when exported and triggered/invoked: %s", command)}
}

// LintSecurityRisks returns a warning for each suspicious shape (see
// suspiciousCommandPatterns) found in the artifact's command.
func (a *Artifact) LintSecurityRisks() []LintWarning {
	command := a.ExtraString("command")
	var warnings []LintWarning
	for _, p := range suspiciousCommandPatterns {
		if p.pattern.MatchString(command) {
			warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf("command %s -- review it carefully before trusting", p.reason)})
		}
	}
	return warnings
}
