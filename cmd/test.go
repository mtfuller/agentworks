package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/mcpclient"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

var testCmd = &cobra.Command{
	Use:   "test [path]",
	Short: "Run an artifact's declared test command",
	Long: `Shell out to the "test:" command declared in an artifact's frontmatter,
from within its directory. Works for any language -- AgentWorks doesn't run
the tests itself, it just invokes what you told it to.

With no path, runs every artifact in the project that declares a test
command; artifacts without one are skipped -- except local mcp artifacts,
which get a built-in smoke test (start the server, run the MCP handshake,
list its tools) when they declare no "test:" of their own.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toRun []*artifact.Artifact

		if len(args) == 1 {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toRun = append(toRun, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Warning("%v", e)
			}
			toRun = found
		}

		ran, failed := 0, 0
		for _, a := range toRun {
			testCommand := a.ExtraString("test")
			if testCommand == "" {
				if smokeEligible(a) {
					ran++
					if err := smokeTestMCP(a); err != nil {
						color.Error("%s: mcp smoke test failed: %v", a.Name, err)
						failed++
					}
				}
				continue
			}
			ran++
			color.Info("Running tests for %s (%s): %s", a.Name, a.Kind, testCommand)

			c := exec.Command("sh", "-c", testCommand)
			c.Dir = a.Dir
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				color.Error("%s: tests failed: %v", a.Name, err)
				failed++
				continue
			}
			color.Success("%s: tests passed", a.Name)
		}

		if ran == 0 {
			color.Info("No artifacts declare a `test:` command.")
		}
		if failed > 0 {
			return fmt.Errorf("%d artifact(s) failed tests", failed)
		}
		return nil
	},
}

// smokeEligible reports whether a is a local mcp server that should get the
// built-in smoke test in place of a declared `test:` command.
func smokeEligible(a *artifact.Artifact) bool {
	return a.Kind == artifact.KindMCP && !mcpconfig.IsRemote(a) && mcpconfig.CommandLine(a) != "" && mcpconfig.Placeholder(a) == ""
}

// smokeTestMCP starts the mcp artifact's server, runs the MCP initialize
// handshake and tools/list, and reports what it found. It's what `agentworks
// test` does for an mcp artifact with no `test:` of its own -- proof the
// server starts and speaks the protocol, not a test of its tools' behavior.
// Skipped (with a warning, not a failure) when a declared `auth:` variable
// isn't set, since the server can't be expected to start without it.
func smokeTestMCP(a *artifact.Artifact) error {
	env := os.Environ()
	for _, name := range a.ExtraStringSlice("auth") {
		if !envHasValue(env, name) {
			color.Warning("%s: skipping mcp smoke test -- %q is not set", a.Name, name)
			return nil
		}
	}

	color.Info("Smoke-testing mcp server %s: %s", a.Name, mcpconfig.CommandLine(a))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var stderr []string
	info, tools, err := mcpclient.Probe(ctx, mcpconfig.CommandLine(a), a.Dir, append(env, mcpEnvPairs(a)...), func(line string) {
		stderr = append(stderr, line)
	})
	if err != nil {
		for _, line := range stderr {
			color.Warning("  stderr: %s", line)
		}
		return err
	}
	color.Success("%s: %s %s started and lists %d tool(s)", a.Name, info.ServerInfo.Name, info.ServerInfo.Version, len(tools))
	return nil
}

func init() {
	rootCmd.AddCommand(testCmd)
}
