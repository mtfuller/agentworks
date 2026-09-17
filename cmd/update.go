package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/lockfile"
)

var (
	updateApply bool
	updateYes   bool
)

var updateCmd = &cobra.Command{
	Use:   "update [path...]",
	Short: "Check artifacts imported with 'agentworks add' for upstream changes",
	Long: `Re-fetch every artifact recorded in agentworks.lock (or just the given
paths) from the source it was imported from, and compare its content
against what was pinned at import time.

With no --apply, this only reports drift -- nothing is written. With
--apply, a changed artifact is overwritten with the freshly fetched
content (its local name/namespace/directory are kept; if upstream itself
renamed the artifact, this fails rather than silently moving it -- re-run
'agentworks add' by hand in that case). The same shell-command confirmation
gate 'agentworks add' uses applies here too when the refreshed content
declares one.`,
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
			newHash, err := fetchAndHash(entry)
			if err != nil {
				color.Error("%s: %v", key, err)
				failed++
				continue
			}

			if newHash == entry.ContentSHA256 {
				color.Success("%s: up to date", key)
				upToDate++
				continue
			}

			changed++
			color.Warning("%s: upstream changed (%s -> %s)", key, shortHash(entry.ContentSHA256), shortHash(newHash))
			if !updateApply {
				continue
			}
			if err := applyUpdate(root, lf, key, entry); err != nil {
				color.Error("%s: %v", key, err)
				failed++
				continue
			}
			color.Success("%s: updated", key)
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

// fetchAndHash re-fetches entry's locked source and content-hashes the
// same subpath within it that was hashed at import time (see
// importer.Plan.HashSources/Subpaths), so the comparison is apples to
// apples regardless of whether entry came from a bare skill or one
// artifact out of a multi-artifact plugin.
func fetchAndHash(entry lockfile.ImportEntry) (string, error) {
	tempDir, err := os.MkdirTemp("", "agentworks-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	contentDir, err := importer.Fetch(context.Background(), importerSource(entry.Source), tempDir)
	if err != nil {
		return "", err
	}
	hashSrc := contentDir
	if entry.SourceSubpath != "" {
		hashSrc = filepath.Join(contentDir, entry.SourceSubpath)
	}
	return lockfile.HashDir(hashSrc)
}

// applyUpdate re-resolves entry's source into a fresh Plan, locates the
// specific artifact this locked entry refers to by its recorded subpath
// (robust to upstream renaming other siblings in the same plugin), and
// overwrites the existing local artifact at key with its content --
// keeping the local directory/name so nothing that already references
// this artifact (a workflow step, another export) breaks silently.
func applyUpdate(root string, lf *lockfile.Lockfile, key string, entry lockfile.ImportEntry) error {
	src := importerSource(entry.Source)
	plan, err := importer.Prepare(context.Background(), root, src, importer.Options{})
	if err != nil {
		return err
	}
	defer plan.Close()

	target, hashSrc, ok := findBySubpath(plan, entry.SourceSubpath)
	if !ok {
		return fmt.Errorf("re-fetched %s no longer has the content this artifact was imported from (expected at %q)", src, entry.SourceSubpath)
	}

	ok2, err := securityGate(src.String(), []*artifact.Artifact{target}, updateYes)
	if err != nil {
		return err
	}
	if !ok2 {
		return fmt.Errorf("update aborted")
	}

	existingDir := filepath.Join(root, filepath.FromSlash(key))
	if err := plan.Overwrite(target, existingDir); err != nil {
		return err
	}

	newHash, err := lockfile.HashDir(hashSrc)
	if err != nil {
		return err
	}
	entry.ContentSHA256 = newHash
	lf.SetImport(key, entry)
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

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateApply, "apply", false, "overwrite artifacts that have changed upstream (default is report-only)")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "skip the confirmation prompt when refreshed content declares a shell command (required in non-interactive use)")
}
