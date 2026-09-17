package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/lockfile"
)

var statusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Check exported dist/ output for drift against its source artifact(s)",
	Long: `Fully offline: compares every export recorded in agentworks.lock (see
'agentworks export') against the current state on disk, and reports each as:

  in sync   both the source artifact(s) and the exported output match what
            was recorded at the last export -- nothing to do.
  modified  the exported output no longer matches what was written --
            it was hand-edited since the last export. Re-exporting will
            discard those edits.
  stale     the source artifact(s) changed since the last export. Re-export
            to pick up the change.
  missing   the recorded output path no longer exists.

Pass a path to filter to exports whose source artifact matches it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		lf, err := lockfile.Load(root)
		if err != nil {
			return err
		}
		if len(lf.Exports) == 0 {
			color.Info("No exports recorded in %s -- run 'agentworks export' first.", lockfile.FileName)
			return nil
		}

		var filter string
		if len(args) == 1 {
			key, err := rootRelKey(root, args[0])
			if err != nil {
				return err
			}
			filter = key
		}

		keys := make([]string, 0, len(lf.Exports))
		for k, e := range lf.Exports {
			if filter != "" && !strings.Contains(e.Artifact, filter) {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			color.Info("No matching exports recorded in %s.", lockfile.FileName)
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "TARGET\tARTIFACT\tOUTPUT\tSTATUS")
		counts := map[string]int{}
		for _, k := range keys {
			e := lf.Exports[k]
			s := exportStatus(root, e)
			counts[s]++
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Target, e.Artifact, e.Output, s)
		}
		w.Flush()

		color.Info("%d in sync, %d modified, %d stale, %d missing", counts["in sync"], counts["modified"], counts["stale"], counts["missing"])
		return nil
	},
}

// exportStatus classifies one ExportEntry against the current on-disk
// state. Checked in this order: a hand-edit is worth flagging even when
// the source has also since changed (re-exporting would discard the edit
// either way), so "modified" takes priority over "stale".
func exportStatus(root string, e lockfile.ExportEntry) string {
	outputHash, err := lockfile.HashDir(e.Output)
	if err != nil {
		return "missing"
	}
	if outputHash != e.OutputSHA256 {
		return "modified"
	}

	relDirs := strings.Split(e.Artifact, "+")
	sourceDirs := make([]string, len(relDirs))
	for i, rel := range relDirs {
		sourceDirs[i] = filepath.Join(root, filepath.FromSlash(rel))
	}
	sourceHash, err := hashArtifactDirs(sourceDirs)
	if err != nil || sourceHash != e.SourceSHA256 {
		return "stale"
	}
	return "in sync"
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
