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

		results := runBuildResults(toRun)
		ran, failed := 0, 0
		doc := buildDoc{Artifacts: []buildItem{}}
		for _, r := range results {
			ran++
			if r.Error != "" {
				failed++
			}
			doc.Artifacts = append(doc.Artifacts, r.buildItem)
		}

		if ran == 0 {
			color.Info("No artifacts declare a `build:` command.")
		}
		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d artifact(s) failed to build", failed)
		}
		if jsonFlag {
			doc.envelope = newEnvelope("build", failErr == nil)
			if err := emitJSON(doc); err != nil {
				return err
			}
		}
		return failErr
	},
}

type buildDoc struct {
	envelope
	Artifacts []buildItem `json:"artifacts"`
}

// buildItem is one artifact whose build command ran. Artifacts declaring no
// build command aren't listed.
type buildItem struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Command string `json:"command"`
	Built   bool   `json:"built"`
	// Error is the failure, when Built is false.
	Error string `json:"error,omitempty"`
}

type buildResult struct {
	buildItem
}

// runBuilds runs each artifact's declared "build:" command from within its
// directory, skipping artifacts that declare none. It reports how many
// builds ran and how many of those failed, and keeps going after a failure
// so one run surfaces every broken build.
func runBuilds(arts []*artifact.Artifact) (ran, failed int) {
	for _, r := range runBuildResults(arts) {
		ran++
		if r.Error != "" {
			failed++
		}
	}
	return ran, failed
}

// runBuildResults is runBuilds with a record of each build that ran.
func runBuildResults(arts []*artifact.Artifact) []buildResult {
	var results []buildResult
	for _, a := range arts {
		buildCommand := a.ExtraString("build")
		if buildCommand == "" {
			continue
		}
		color.Info("Building %s (%s): %s", a.Name, a.Kind, buildCommand)
		r := buildResult{buildItem{Kind: string(a.Kind), Name: a.Name, Path: itemPath(a), Command: buildCommand}}

		c := exec.Command("sh", "-c", buildCommand)
		c.Dir = a.Dir
		c.Stdout = stdoutForChildren()
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			color.Error("%s: build failed: %v", a.Name, err)
			r.Error = err.Error()
		} else {
			r.Built = true
			color.Success("%s: build succeeded", a.Name)
		}
		results = append(results, r)
	}
	return results
}

func init() {
	rootCmd.AddCommand(buildCmd)
}
