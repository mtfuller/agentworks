package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/lockfile"
)

var (
	updateApply bool
	updateYes   bool
	updateForce bool
	updateDiff  bool
)

var updateCmd = &cobra.Command{
	Use:   "update [path...]",
	Short: "Check artifacts imported with 'agentworks add' for upstream changes",
	Long: `Re-fetch every artifact recorded in agentworks.lock (or just the given
paths) from the source it was imported from, and compare its content
against what was pinned at import time.

With no --apply, this only reports drift -- nothing is written. --diff also
prints what --apply would change, as a diff against your local copy. With
--apply, a changed artifact is overwritten with the freshly fetched
content (its local name/namespace/directory are kept; if upstream itself
renamed the artifact, this fails rather than silently moving it -- re-run
'agentworks add' by hand in that case). The same shell-command confirmation
gate 'agentworks add' uses applies here too when the refreshed content
declares one.

If you have edited an imported artifact since it was imported, --apply
refuses to overwrite it, since that would discard your changes; --force
overrides that. Use --diff first to see what would be lost.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		lf, err := lockfile.Load(root)
		if err != nil {
			return err
		}
		if len(lf.Imports) == 0 {
			color.Info("No imported artifacts recorded in %s -- nothing to check.", lockfile.FileName)
			return nil
		}

		keys, err := resolveUpdateKeys(root, lf, args)
		if err != nil {
			return err
		}

		var upToDate, changed, failed int
		for _, key := range keys {
			entry := lf.Imports[key]
			res, err := checkImport(root, key, entry)
			if err != nil {
				color.Error("%s: %v", key, err)
				failed++
				continue
			}
			func() {
				defer res.plan.Close()

				if res.newHash == entry.ContentSHA256 {
					color.Success("%s: up to date", key)
					upToDate++
					return
				}

				changed++
				color.Warning("%s: upstream changed (%s)", key, describeChange(entry, res))
				edited := locallyEdited(root, key, entry)
				if edited {
					color.Warning("%s: you have edited this since it was imported", key)
				}
				if updateDiff {
					if err := printUpdateDiff(root, key, res); err != nil {
						color.Warning("%s: couldn't produce a diff: %v", key, err)
					}
				}
				if !updateApply {
					return
				}
				if edited && !updateForce {
					color.Error("%s: not updated -- it has local changes that --apply would discard (re-run with --diff to see them, --force to overwrite)", key)
					failed++
					return
				}
				if err := applyUpdate(root, lf, key, entry, res); err != nil {
					color.Error("%s: %v", key, err)
					failed++
					return
				}
				color.Success("%s: updated", key)
			}()
		}

		if err := lf.Save(root); err != nil {
			color.Warning("failed to update %s: %v", lockfile.FileName, err)
		}

		color.Info("%d up to date, %d changed, %d failed", upToDate, changed, failed)
		if failed > 0 {
			return fmt.Errorf("%d artifact(s) failed to check/update", failed)
		}
		return nil
	},
}

// resolveUpdateKeys turns update's path arguments into agentworks.lock
// import keys, defaulting to every locked import when none are given.
// Accepts either the key exactly as it appears in the lockfile (what
// `agentworks list`/the lockfile itself shows) or a filesystem path to the
// same artifact directory.
func resolveUpdateKeys(root string, lf *lockfile.Lockfile, args []string) ([]string, error) {
	if len(args) == 0 {
		keys := make([]string, 0, len(lf.Imports))
		for k := range lf.Imports {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys, nil
	}

	keys := make([]string, 0, len(args))
	for _, arg := range args {
		key := arg
		if _, ok := lf.Imports[key]; !ok {
			abs, err := filepath.Abs(arg)
			if err != nil {
				return nil, err
			}
			if k, err := lockfile.RelKey(root, abs); err == nil {
				key = k
			}
		}
		if _, ok := lf.Imports[key]; !ok {
			return nil, fmt.Errorf("%s: not recorded in %s (not imported with 'agentworks add', or already up to date with nothing pinned)", arg, lockfile.FileName)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// importCheck is a freshly fetched view of one locked import: the re-resolved
// plan, the artifact within it this lock entry refers to, and that artifact's
// upstream content hash.
type importCheck struct {
	plan    *importer.Plan
	target  *artifact.Artifact
	newHash string
}

// checkImport re-fetches entry's locked source, finds the same artifact again
// by its recorded subpath (robust to upstream renaming sibling artifacts in
// the same plugin), and hashes the same content that was hashed at import
// time -- so the comparison is apples to apples whether entry came from a bare
// skill, a plugin's skill or agent, or an MCP server or hook entry in a
// plugin's JSON. The caller closes res.plan.
func checkImport(root, key string, entry lockfile.ImportEntry) (*importCheck, error) {
	src := importerSource(entry.Source)
	// Re-plan under the namespace the artifact already lives in. Without this
	// the plan falls back to the source's default namespace, which differs
	// whenever the artifact was added with --namespace, and the refreshed
	// artifact would then fail to validate against its own directory.
	plan, err := importer.Prepare(context.Background(), root, src, importer.Options{Namespace: namespaceOfKey(key)})
	if err != nil {
		return nil, err
	}
	target, hashSrc, ok := findBySubpath(plan, entry.SourceSubpath)
	if !ok {
		plan.Close()
		return nil, fmt.Errorf("re-fetched %s no longer has the content this artifact was imported from (expected at %q)", src, entry.SourceSubpath)
	}
	newHash, err := lockfile.HashDir(hashSrc)
	if err != nil {
		plan.Close()
		return nil, err
	}
	return &importCheck{plan: plan, target: target, newHash: newHash}, nil
}

// namespaceOfKey extracts the namespace from a lockfile import key such as
// "mcp/kit/db" (kind directory, namespace, name). A key with no namespace
// level -- an artifact imported before imports were namespaced -- yields "",
// which Prepare treats as "use the source's default".
func namespaceOfKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) == 3 {
		return parts[1]
	}
	return ""
}

// locallyEdited reports whether the artifact at key has changed on disk since
// it was last imported or updated. An entry with no recorded local hash
// (written before it existed) or an artifact that can't be read is treated as
// unedited, so this never blocks on missing information.
func locallyEdited(root, key string, entry lockfile.ImportEntry) bool {
	if entry.LocalSHA256 == "" {
		return false
	}
	current, err := lockfile.HashDir(filepath.Join(root, filepath.FromSlash(key)))
	return err == nil && current != entry.LocalSHA256
}

// applyUpdate overwrites the existing local artifact at key with the freshly
// fetched content, keeping the local directory/name so nothing that already
// references this artifact (another export, a `requires:`) breaks silently.
func applyUpdate(root string, lf *lockfile.Lockfile, key string, entry lockfile.ImportEntry, res *importCheck) error {
	src := importerSource(entry.Source)
	ok, err := securityGate(src.String(), []*artifact.Artifact{res.target}, updateYes)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("update aborted")
	}

	existingDir := filepath.Join(root, filepath.FromSlash(key))
	if err := res.plan.Overwrite(res.target, existingDir); err != nil {
		return err
	}
	for _, w := range res.plan.Warnings {
		color.Warning("%s", w)
	}

	local, err := lockfile.HashDir(existingDir)
	if err != nil {
		return err
	}
	entry.ContentSHA256 = res.newHash
	entry.LocalSHA256 = local
	entry.Commit = res.plan.Commit
	lf.SetImport(key, entry)
	return nil
}

// printUpdateDiff shows what --apply would do to the local artifact: it
// renders the upstream artifact into a scratch directory exactly as Overwrite
// would write it, then diffs that against the local copy. Uses the system
// diff; without one it lists nothing rather than guessing.
func printUpdateDiff(root, key string, res *importCheck) error {
	scratch, err := os.MkdirTemp("", "agentworks-update-diff-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	// Render a copy: Overwrite mutates the artifact's Dir, and the real apply
	// may still follow.
	clone := *res.target
	// Keep the artifact's <kind>/<namespace>/<name> layout: Overwrite validates
	// the artifact against the directory it is written into.
	rendered := filepath.Join(scratch, filepath.FromSlash(key))
	if err := res.plan.Overwrite(&clone, rendered); err != nil {
		return err
	}

	diffBin, err := exec.LookPath("diff")
	if err != nil {
		return fmt.Errorf("no diff binary on PATH")
	}
	local := filepath.Join(root, filepath.FromSlash(key))
	out, err := exec.Command(diffBin, "-ruN", "--label", "local/"+key, "--label", "upstream/"+key, local, rendered).CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return fmt.Errorf("diff failed: %v", err)
		}
	}
	// diff prints absolute directory names in its headers for a -r run; the
	// labels above only cover the files' own header lines, so strip the rest.
	fmt.Print(strings.ReplaceAll(strings.ReplaceAll(string(out), local, "local/"+key), rendered, "upstream/"+key))
	return nil
}

// findBySubpath returns the artifact in plan whose Subpaths entry matches
// subpath, along with its HashSources location (captured before any
// caller mutates the artifact's Dir).
func findBySubpath(plan *importer.Plan, subpath string) (a *artifact.Artifact, hashSrc string, ok bool) {
	for _, cand := range plan.Artifacts {
		if plan.Subpaths[cand.Dir] == subpath {
			return cand, plan.HashSources[cand.Dir], true
		}
	}
	return nil, "", false
}

// describeChange says what moved: the commit, when both sides know it, and
// otherwise the content hash.
func describeChange(entry lockfile.ImportEntry, res *importCheck) string {
	if entry.Commit != "" && res.plan.Commit != "" && entry.Commit != res.plan.Commit {
		return "commit " + shortHash(entry.Commit) + " -> " + shortHash(res.plan.Commit)
	}
	return "content " + shortHash(entry.ContentSHA256) + " -> " + shortHash(res.newHash)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "with --apply, overwrite an artifact even if you have edited it since it was imported")
	updateCmd.Flags().BoolVar(&updateDiff, "diff", false, "print what --apply would change, as a diff against your local copy")
	updateCmd.Flags().BoolVar(&updateApply, "apply", false, "overwrite artifacts that have changed upstream (default is report-only)")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "skip the confirmation prompt when refreshed content declares a shell command (required in non-interactive use)")
}
