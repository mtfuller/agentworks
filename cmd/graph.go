package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
)

var graphDot bool

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Show which artifacts require which",
	Long: `Print the dependency graph declared by "requires:" -- what each artifact
depends on, and what depends on it. With --dot, print Graphviz DOT instead
(pipe it to "dot -Tsvg").`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		found, errs := project.Discover(root)
		for _, e := range errs {
			color.Warning("%v", e)
		}

		if graphDot {
			fmt.Print(dotGraph(found))
			return nil
		}
		usedBy := map[artifact.Ref][]string{}
		for _, a := range found {
			for _, r := range a.Requires() {
				usedBy[r] = append(usedBy[r], a.Ref().String())
			}
		}
		any := false
		for _, a := range found {
			reqs := refStrings(a.Requires())
			users := usedBy[a.Ref()]
			if len(reqs) == 0 && len(users) == 0 {
				continue
			}
			any = true
			sort.Strings(users)
			fmt.Printf("%s\n", a.Ref())
			if len(reqs) > 0 {
				fmt.Printf("  requires: %s\n", strings.Join(reqs, ", "))
			}
			if len(users) > 0 {
				fmt.Printf("  required by: %s\n", strings.Join(users, ", "))
			}
		}
		if !any {
			color.Info("No artifact declares `requires:`.")
		}
		return nil
	},
}

// dotGraph renders the requires: edges as a Graphviz digraph.
func dotGraph(arts []*artifact.Artifact) string {
	var b strings.Builder
	b.WriteString("digraph agentworks {\n  rankdir=LR;\n")
	for _, a := range arts {
		fmt.Fprintf(&b, "  %q [shape=box];\n", a.Ref().String())
	}
	for _, a := range arts {
		for _, r := range a.Requires() {
			fmt.Fprintf(&b, "  %q -> %q;\n", a.Ref().String(), r.String())
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func init() {
	graphCmd.Flags().BoolVar(&graphDot, "dot", false, "print Graphviz DOT instead of text")
	rootCmd.AddCommand(graphCmd)
}
