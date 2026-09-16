package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
)

var testCmd = &cobra.Command{
	Use:   "test [path]",
	Short: "Run an artifact's declared test command",
	Long: `Shell out to the "test:" command declared in an artifact's frontmatter,
from within its directory. Works for any language -- AgentWorks doesn't run
the tests itself, it just invokes what you told it to.

With no path, runs every artifact in the project that declares a test
command; artifacts without one are skipped.`,
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

func init() {
	rootCmd.AddCommand(testCmd)
}
