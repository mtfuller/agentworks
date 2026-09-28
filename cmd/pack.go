package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/pack"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/providers"
)

var packOutput string

var packCmd = &cobra.Command{
	Use:   "pack <team>",
	Short: "Create a deterministic, platform-independent agent-team archive",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := project.FindRoot(projectFlag)
		if err != nil {
			return fmt.Errorf("%w (pass --project with a format-2 project)", err)
		}
		output := packOutput
		if output != "" && !filepath.IsAbs(output) {
			output = filepath.Join(root, output)
		}
		result, err := pack.Create(root, args[0], output)
		if err != nil {
			return err
		}
		if jsonFlag {
			return emitJSON(packDoc{envelope: newEnvelope("pack", true), Action: "create", Result: &result, Ready: true, ProviderReadiness: []providers.ToolReadiness{}})
		}
		fmt.Printf("Packed %s/%s\n", result.Plan.Project, result.Plan.Team)
		fmt.Printf("Archive: %s\n", result.Path)
		fmt.Printf("SHA-256: %s\n", result.ArchiveDigest)
		fmt.Printf("Files: %d\n", result.Files)
		return nil
	},
}

var packInstallCmd = &cobra.Command{
	Use:   "install <archive> <destination>",
	Short: "Verify and install an agent-team archive",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		destination, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		manifest, err := pack.Install(args[0], destination)
		if err != nil {
			return err
		}
		readiness, readinessErr := providers.InspectTeam(cmd.Context(), destination, manifest.Team, providers.Probe{})
		ready := readinessErr == nil
		for _, tool := range readiness {
			ready = ready && tool.Selected != nil
		}
		if jsonFlag {
			return emitJSON(packDoc{envelope: newEnvelope("pack", true), Action: "install", Manifest: &manifest, Destination: destination, Ready: ready, ProviderReadiness: readiness})
		}
		fmt.Printf("Installed %s/%s\n", manifest.Project, manifest.Team)
		fmt.Printf("Destination: %s\n", destination)
		fmt.Printf("Content SHA-256: %s\n", manifest.ContentDigest)
		if readinessErr != nil {
			fmt.Printf("Provider readiness: unavailable (%v)\n", readinessErr)
		} else if ready {
			fmt.Println("Provider readiness: ready")
		} else {
			fmt.Println("Provider readiness: no compatible runtime is currently ready; run agentworks providers <team> for diagnostics")
		}
		return nil
	},
}

type packDoc struct {
	envelope
	Action            string                    `json:"action"`
	Result            *pack.Result              `json:"result,omitempty"`
	Manifest          *pack.Manifest            `json:"manifest,omitempty"`
	Destination       string                    `json:"destination,omitempty"`
	Ready             bool                      `json:"ready"`
	ProviderReadiness []providers.ToolReadiness `json:"provider_readiness"`
}

func init() {
	packCmd.Flags().StringVarP(&packOutput, "output", "o", "", "archive path (default dist/packs/<project>-<team>-<digest>.agentworks)")
	packCmd.AddCommand(packInstallCmd)
	rootCmd.AddCommand(packCmd)
}
