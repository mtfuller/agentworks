package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/inspector"
)

var runCmd = &cobra.Command{
	Use:   "run <path>",
	Short: "Start a tool's MCP server and inspect it interactively",
	Long: `Start a tool artifact's declared "command" as a real MCP server and open a
full-screen inspector: browse the tools it exposes, fill in and submit a call
against one, and see the result -- the same way an agent actually would,
instead of only unit-testing the tool's logic with mocked calls.

Only tool artifacts are MCP servers -- skills/agents/hooks/workflows aren't
supported. Unlike 'agentworks export', this actually executes the command,
so it inherits the real environment (including any "auth:" variables already
exported in your shell), not the "${VAR}" placeholders export generates.
Run 'agentworks doctor' first if you're not sure the command/environment is
even set up to run.`,
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

		return inspector.Run(a, a.ExtraString("command"), a.Dir, env, missingAuth)
	},
}

// checkRunnable validates that a is something `agentworks run` can
// actually start, ahead of the non-interactive-terminal check and the
// inspector itself -- kept separate from RunE so it's unit-testable
// without launching a full-screen Bubble Tea program (see cmd/run_test.go).
func checkRunnable(a *artifact.Artifact) error {
	if a.Kind != artifact.KindTool {
		return fmt.Errorf("%s is a %s, not a tool -- agentworks run only supports tool artifacts (skills/agents/hooks/workflows aren't MCP servers)", a.Dir, a.Kind)
	}
	if a.ExtraString("command") == "" {
		return fmt.Errorf("%s has no \"command\" set in its frontmatter -- add one describing how to run it before running it (see 'agentworks doctor')", a.Dir)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(runCmd)
}
