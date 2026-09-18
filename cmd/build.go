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

var buildCmd = &cobra.Command{
	Use:   "build [path]",
	Short: "Run an artifact's declared build command",
	Long: `Shell out to the "build:" command declared in an artifact's frontmatter,
from within its directory. Works for any language -- AgentWorks doesn't build
anything itself, it just invokes what you told it to (installing dependencies,
compiling, bundling, or whatever else the artifact needs before it can run or
be exported).

With no path, runs every artifact in the project that declares a build
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

		ran, failed := runBuilds(toRun)

		if ran == 0 {
			color.Info("No artifacts declare a `build:` command.")
		}
		if failed > 0 {
			return fmt.Errorf("%d artifact(s) failed to build", failed)
		}
		return nil
	},
}

// runBuilds runs each artifact's declared "build:" command from within its
// directory, skipping artifacts that declare none. It reports how many
// builds ran and how many of those failed, and keeps going after a failure
// so one run surfaces every broken build.
func runBuilds(arts []*artifact.Artifact) (ran, failed int) {
	for _, a := range arts {
		buildCommand := a.ExtraString("build")
		if buildCommand == "" {
			continue
		}
		ran++
		color.Info("Building %s (%s): %s", a.Name, a.Kind, buildCommand)

		c := exec.Command("sh", "-c", buildCommand)
		c.Dir = a.Dir
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			color.Error("%s: build failed: %v", a.Name, err)
			failed++
			continue
		}
		color.Success("%s: build succeeded", a.Name)
	}
	return ran, failed
}

func init() {
	rootCmd.AddCommand(buildCmd)
}
