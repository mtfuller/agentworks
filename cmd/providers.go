package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/providers"
)

var providersCmd = &cobra.Command{
	Use:   "providers <team>",
	Short: "Check host, Docker, and remote runtime readiness for a team",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := project.FindRoot(projectFlag)
		if err != nil {
			return fmt.Errorf("%w (pass --project with a format-2 project)", err)
		}
		tools, err := providers.InspectTeam(cmd.Context(), root, args[0], providers.Probe{})
		if err != nil {
			return err
		}
		ready := true
		for _, tool := range tools {
			ready = ready && tool.Selected != nil
		}
		if jsonFlag {
			if err := emitJSON(providersDoc{envelope: newEnvelope("providers", ready), Team: args[0], Ready: ready, Tools: tools}); err != nil {
				return err
			}
			if !ready {
				return fmt.Errorf("one or more tools have no ready execution provider")
			}
			return nil
		}
		writer := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "TOOL\tPROVIDER\tSTATE\tRUNTIME\tDETAIL")
		for _, tool := range tools {
			for _, variant := range tool.Variants {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", tool.Name, variant.Provider, variant.State, variant.Runtime, variant.Detail)
			}
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("one or more tools have no ready execution provider")
		}
		return nil
	},
}

type providersDoc struct {
	envelope
	Team  string                    `json:"team"`
	Ready bool                      `json:"ready"`
	Tools []providers.ToolReadiness `json:"tools"`
}

func init() { rootCmd.AddCommand(providersCmd) }
