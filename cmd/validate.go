package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/workflowsteps"
)

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate artifact frontmatter",
	Long:  "Parse and validate one artifact (by path) or every artifact in the project.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toCheck []*artifact.Artifact
		discoverErrs := 0

		if len(args) == 1 {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toCheck = append(toCheck, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Error("%v", e)
			}
			discoverErrs = len(errs)
			toCheck = found
		}

		failed := discoverErrs
		for _, a := range toCheck {
			if err := a.Validate(); err != nil {
				color.Error("%v", err)
				failed++
				continue
			}
			if a.Kind == artifact.KindWorkflow {
				if _, err := workflowsteps.Resolve(a); err != nil {
					color.Error("%v", err)
					failed++
					continue
				}
			}
			color.Success("%s (%s)", a.Name, a.Kind)
		}

		if failed > 0 {
			return fmt.Errorf("%d artifact(s) failed validation", failed)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
