package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/inspector"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

var runCmd = &cobra.Command{
	Use:   "run <path>",
	Short: "Connect to an mcp artifact's server and inspect it interactively",
	Long: `Connect to an mcp artifact's server -- starting its declared "command" as a real
process, or reaching its remote http/sse "url" -- and open a full-screen
inspector: browse the tools it exposes, fill in and submit a call against one,
and see the result, the same way an agent actually would, instead of only
unit-testing the server's logic with mocked calls. A server that also offers
resources or prompts gets a section for each (keys 1, 2, 3).

Only mcp artifacts are supported. Unlike 'agentworks export', this actually
connects, so it uses the real environment: a local server inherits it
(including any "auth:" variables already exported in your shell), and a
remote server's headers have their "${VAR}" references expanded from it -- not
the placeholders export generates. Run 'agentworks doctor' first if you're not
sure the command/environment is even set up.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadArtifactAtPath(args[0])
		if err != nil {
			return err
		}
		if err := checkRunnable(a); err != nil {
			return err
		}
		if !isInteractive() {
			return fmt.Errorf("agentworks run needs an interactive terminal -- run it directly, not piped or from a script")
		}

		env := os.Environ()
		var missingAuth []string
		for _, name := range a.ExtraStringSlice("auth") {
			if !envHasValue(env, name) {
				missingAuth = append(missingAuth, name)
			}
		}

		target, missingHeaderVars := mcpTarget(a)
		for _, name := range missingHeaderVars {
			if !containsString(missingAuth, name) {
				missingAuth = append(missingAuth, name)
			}
		}
		return inspector.Run(a, target, missingAuth)
	},
}

// checkRunnable validates that a is something `agentworks run` can
// actually start, ahead of the non-interactive-terminal check and the
// inspector itself -- kept separate from RunE so it's unit-testable
// without launching a full-screen Bubble Tea program (see cmd/run_test.go).
func checkRunnable(a *artifact.Artifact) error {
	if a.Kind != artifact.KindMCP {
		return fmt.Errorf("%s is a %s, not an mcp server -- agentworks run only supports mcp artifacts", a.Dir, a.Kind)
	}
	if mcpconfig.IsRemote(a) {
		if a.ExtraString("url") == "" {
			return fmt.Errorf("%s is a remote (%s) server with no \"url\" set in its frontmatter", a.Dir, mcpconfig.TransportOf(a))
		}
		return nil
	}
	if a.ExtraString("command") == "" {
		return fmt.Errorf("%s has no \"command\" set in its frontmatter -- add one describing how to run it before running it (see 'agentworks doctor')", a.Dir)
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mcpEnvPairs renders an mcp artifact's literal `env:` block as KEY=value
// pairs to append to the process environment. (`auth:` variables are read
// from the caller's real environment instead, never from frontmatter.)
func mcpEnvPairs(a *artifact.Artifact) []string {
	var pairs []string
	for k, v := range a.ExtraStringMap("env") {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return pairs
}

func init() {
	rootCmd.AddCommand(runCmd)
}
