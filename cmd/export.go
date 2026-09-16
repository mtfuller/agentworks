package cmd

import (
	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/targets"

	// Blank-imported so its init() registers the "claude-code" exporter
	// with internal/targets. Add further vendor exporter packages here as
	// they're implemented.
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
)

var (
	exportTarget string
	exportOut    string
	exportZip    bool
)

var exportCmd = &cobra.Command{
	Use:   "export <path>",
	Short: "Export an artifact to a vendor target's native format",
	Long: `Transform an artifact into a vendor's native format under --out (default:
./dist). Run "agentworks targets" to see which vendor targets have a real
exporter implemented yet.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadArtifactAtPath(args[0])
		if err != nil {
			return err
		}

		exporter, err := targets.GetExporter(exportTarget)
		if err != nil {
			return err
		}

		out, err := exporter.Export(a, exportOut, targets.ExportOptions{Zip: exportZip})
		if err != nil {
			return err
		}

		color.Success("Exported %s (%s) to %s for %s", a.Name, a.Kind, out, exportTarget)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportTarget, "target", "", "vendor target to export to (see 'agentworks targets')")
	exportCmd.Flags().StringVar(&exportOut, "out", "dist", "output directory")
	exportCmd.Flags().BoolVar(&exportZip, "zip", false, "also package the export as a .zip")
	_ = exportCmd.MarkFlagRequired("target")
}
