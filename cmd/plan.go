package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
)

var planProviders []string

var planCmd = &cobra.Command{
	Use:   "plan <team>",
	Short: "Resolve a format-2 agent team and its shared capabilities",
	Long: `Resolve a format-2 team into the exact agents, skills, tools, and runtime
variants it would receive. The operation is read-only and vendor-neutral; it does not
start a harness or materialize missing dependencies.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := project.FindRoot(projectFlag)
		if err != nil {
			return fmt.Errorf("%w (pass --project with a format-2 project)", err)
		}
		providers, err := parsePlanProviders(planProviders)
		if err != nil {
			return err
		}
		resolved, err := resolver.ResolveTeam(root, args[0], resolver.Options{AvailableProviders: providers})
		if err != nil {
			return err
		}
		if jsonFlag {
			return emitJSON(planDoc{envelope: newEnvelope("plan", true), Plan: *resolved})
		}
		return printPlan(resolved)
	},
}

type planDoc struct {
	envelope
	Plan resolver.TeamPlan `json:"plan"`
}

func parsePlanProviders(values []string) ([]spec.Provider, error) {
	if len(values) == 0 {
		values = []string{string(spec.ProviderHost)}
	}
	providers := make([]spec.Provider, 0, len(values))
	seen := map[spec.Provider]struct{}{}
	for _, value := range values {
		provider := spec.Provider(value)
		if !provider.Valid() {
			return nil, fmt.Errorf("unknown provider %q (choose host, container, or remote)", value)
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		providers = append(providers, provider)
	}
	return providers, nil
}

func printPlan(plan *resolver.TeamPlan) error {
	fmt.Printf("Resolved %s/%s\n", plan.Project, plan.Team)
	fmt.Printf("Digest: %s\n\n", plan.Digest)

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tPERMISSION\tSKILLS\tTOOLS")
	for _, agent := range plan.Agents {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", agent.Name, agent.MaxPermission, strings.Join(agent.Skills, ", "), strings.Join(agent.Tools, ", "))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(plan.Skills) > 0 {
		fmt.Println("\nSkills:")
		for _, skill := range plan.Skills {
			fmt.Printf("  %s (%s)\n", skill.Name, skill.Origin)
		}
	}
	if len(plan.Tools) > 0 {
		fmt.Println("\nTools:")
		tools := append([]resolver.ToolPlan(nil), plan.Tools...)
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, tool := range tools {
			fmt.Printf("  %s (%s via %s)\n", tool.Name, tool.Transport, tool.SelectedProvider)
		}
	}
	return nil
}

func init() {
	planCmd.Flags().StringSliceVar(&planProviders, "provider", []string{"host"}, "available execution provider (host, container, remote; repeatable)")
	rootCmd.AddCommand(planCmd)
}
