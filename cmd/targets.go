package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/targets"
)

var targetsCmd = &cobra.Command{
	Use:   "targets",
	Short: "Show which artifact kinds each vendor target supports",
	Long: `Print the capability matrix: which artifact kinds each known vendor
target can consume, and whether AgentWorks has a real exporter for it yet.
Check this before investing effort in an artifact meant for a specific vendor.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all := targets.All()

		if jsonFlag {
			doc := targetsDoc{envelope: newEnvelope("targets", true), Targets: make([]targetItem, 0, len(all))}
			for _, t := range all {
				item := targetItem{ID: t.ID, Name: t.Name, Notes: t.Notes, Format: t.Format, Verified: t.Verified, Supports: []string{}}
				for _, k := range artifact.Kinds() {
					if targets.Supports(t.ID, k) {
						item.Supports = append(item.Supports, string(k))
					}
				}
				_, err := targets.GetExporter(t.ID)
				item.ExportImplemented = err == nil
				doc.Targets = append(doc.Targets, item)
			}
			return emitJSON(doc)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		header := "KIND"
		for _, t := range all {
			header += "\t" + t.Name
		}
		fmt.Fprintln(w, header)
		for _, k := range artifact.Kinds() {
			row := string(k)
			for _, t := range all {
				if targets.Supports(t.ID, k) {
					row += "\t✓"
				} else {
					row += "\t·"
				}
			}
			fmt.Fprintln(w, row)
		}
		if err := w.Flush(); err != nil {
			return err
		}

		fmt.Println()
		for _, t := range all {
			status := "no exporter yet"
			if _, err := targets.GetExporter(t.ID); err == nil {
				status = "export implemented"
			}
			color.Info("%s (%s) — %s. %s", t.Name, t.ID, status, t.Notes)
			color.Info("  format: %s (verified %s)", t.Format, t.Verified)
		}
		return nil
	},
}

type targetsDoc struct {
	envelope
	Targets []targetItem `json:"targets"`
}

type targetItem struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Supports          []string `json:"supports"`
	ExportImplemented bool     `json:"export_implemented"`
	Notes             string   `json:"notes"`
	Format            string   `json:"format"`
	Verified          string   `json:"verified"`
}

func init() {
	rootCmd.AddCommand(targetsCmd)
}
