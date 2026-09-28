package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/harness/claudecode"
	"github.com/mtfuller/agentworks/internal/harness/githubcopilot"
)

var harnessesCmd = &cobra.Command{
	Use:   "harnesses",
	Short: "Probe headless agent harness readiness without making a model request",
	Long: `Probe the initial Claude Code and GitHub Copilot CLI adapters for installation,
programmatic interface compatibility, and authentication configuration. This command does
not submit a prompt or consume model usage.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		results := probeHarnesses(cmd.Context(), []harness.Adapter{
			claudecode.New(nil),
			githubcopilot.New(nil),
		})
		items := make([]harnessProbeItem, 0, len(results))
		for _, result := range results {
			capabilities := make([]string, len(result.Capabilities))
			for index, capability := range result.Capabilities {
				capabilities[index] = string(capability)
			}
			items = append(items, harnessProbeItem{
				ID: string(result.Harness), Status: string(result.Status), Executable: result.Executable,
				Version: result.Version, AuthMethod: result.AuthMethod,
				Capabilities: capabilities, Diagnostics: result.Diagnostics,
			})
		}
		if jsonFlag {
			return emitJSON(harnessesDoc{envelope: newEnvelope("harnesses", true), Harnesses: items})
		}
		return printHarnesses(items)
	},
}

type harnessesDoc struct {
	envelope
	Harnesses []harnessProbeItem `json:"harnesses"`
}

type harnessProbeItem struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Executable   string   `json:"executable,omitempty"`
	Version      string   `json:"version,omitempty"`
	AuthMethod   string   `json:"auth_method,omitempty"`
	Capabilities []string `json:"capabilities"`
	Diagnostics  []string `json:"diagnostics"`
}

func probeHarnesses(ctx context.Context, adapters []harness.Adapter) []harness.ProbeResult {
	results := make([]harness.ProbeResult, len(adapters))
	var wait sync.WaitGroup
	wait.Add(len(adapters))
	for index, adapter := range adapters {
		go func() {
			defer wait.Done()
			results[index] = adapter.Probe(ctx)
		}()
	}
	wait.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Harness < results[j].Harness })
	return results
}

func printHarnesses(items []harnessProbeItem) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "HARNESS\tSTATUS\tVERSION\tCAPABILITIES")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Version, strings.Join(item.Capabilities, ", "))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, item := range items {
		for _, diagnostic := range item.Diagnostics {
			fmt.Printf("%s: %s\n", item.ID, diagnostic)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(harnessesCmd)
}
