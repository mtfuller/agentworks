package cmd

import (
	"fmt"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/version"
	"github.com/spf13/cobra"
)

var short bool

// versionDoc is version's --json document. ProjectFormat is the highest
// agentworks.yaml `format:` this build can read (see project.CurrentFormat).
type versionDoc struct {
	envelope
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuildDate     string `json:"build_date"`
	ProjectFormat int    `json:"project_format"`
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Print the version, commit hash, and build date of this CLI application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonFlag {
			return emitJSON(versionDoc{
				envelope:      newEnvelope("version", true),
				Version:       version.Version,
				Commit:        version.Commit,
				BuildDate:     version.BuildDate,
				ProjectFormat: project.CurrentFormat,
			})
		}
		if short {
			fmt.Println(version.GetShortVersion())
		} else {
			fmt.Println(color.Bold("agentworks"))
			fmt.Printf("Version:    %s\n", color.Cyan(version.Version))
			fmt.Printf("Commit:     %s\n", version.Commit)
			fmt.Printf("Built:      %s\n", version.BuildDate)
			fmt.Printf("Format:     %d (project format this build reads and writes)\n", project.CurrentFormat)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVarP(&short, "short", "s", false, "print only the version number")
}
