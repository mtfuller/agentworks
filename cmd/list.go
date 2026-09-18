package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
)

var listCmd = &cobra.Command{
	Use:   "list [kind]",
	Short: "List artifacts in the project",
	Long:  "List discovered agents, skills, mcp servers, and hooks. Pass a kind to filter.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}

		var kinds []artifact.Kind
		if len(args) == 1 {
			k, err := artifact.ParseKind(args[0])
			if err != nil {
				return err
			}
			kinds = []artifact.Kind{k}
		}

		found, errs := project.Discover(root, kinds...)
		for _, e := range errs {
			color.Warning("%v", e)
		}
		if jsonFlag {
			doc := listDoc{envelope: newEnvelope("list", true), Artifacts: make([]listItem, 0, len(found))}
			for _, a := range found {
				doc.Artifacts = append(doc.Artifacts, listItem{
					Kind:          string(a.Kind),
					Name:          a.Name,
					Namespace:     a.Namespace,
					QualifiedName: a.QualifiedName(),
					Description:   a.Description,
					Version:       a.Version,
					Path:          itemPath(a),
				})
			}
			return emitJSON(doc)
		}
		if len(found) == 0 {
			color.Info("No artifacts found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tNAME\tDESCRIPTION")
		for _, a := range found {
			fmt.Fprintf(w, "%s\t%s\t%s\n", a.Kind, a.DisplayName(), truncate(a.Description, 60))
		}
		return w.Flush()
	},
}

type listDoc struct {
	envelope
	Artifacts []listItem `json:"artifacts"`
}

type listItem struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Namespace     string `json:"namespace,omitempty"`
	QualifiedName string `json:"qualified_name"`
	Description   string `json:"description"`
	Version       string `json:"version,omitempty"`
	Path          string `json:"path"`
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func init() {
	rootCmd.AddCommand(listCmd)
}
