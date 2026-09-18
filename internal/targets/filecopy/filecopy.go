// Package filecopy holds the file operations every export target needs
// regardless of vendor format: copying an artifact's supporting files into
// an export directory (skipping its AgentWorks <kind>.md manifest, which
// each exporter translates into its own vendor file), zipping a directory
// for upload, and -- for the reverse direction -- extracting a fetched
// zip/tar.gz archive (see extract.go) so an importer has something to copy
// files back out of.
package filecopy

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// CopyArtifactFiles copies everything in the artifact's directory into
// destDir, except its AgentWorks manifest (e.g. skill.md, tool.md) --
// callers write that manifest's vendor-format translation separately.
// destDir is created if it doesn't exist.
func CopyArtifactFiles(a *artifact.Artifact, destDir string) error {
	return CopyDirExcept(a.Dir, destDir, a.Kind.FileName())
}

// alwaysSkippedDirs are directory names never copied into an export/import
// destination, regardless of the exclude list a caller passes in --
// reinstallable dependency trees and VCS metadata that would otherwise get
// zipped and shipped verbatim into every vendor package the first time
// someone runs "npm install" (or the Python equivalent) inside an
// artifact's own directory.
var alwaysSkippedDirs = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	".git":         true,
}

// alwaysSkippedFiles are file names never copied, for the same reason as
// alwaysSkippedDirs but for entries that aren't directories.
var alwaysSkippedFiles = map[string]bool{
	".DS_Store": true,
}

// CopyDirExcept copies everything in srcDir into destDir, skipping any
// entry whose path relative to srcDir exactly matches one of exclude (e.g.
// a vendor manifest file being replaced by a different format, or a format
// marker like "SKILL.md" that an importer is translating rather than
// copying verbatim), plus anything in alwaysSkippedDirs/alwaysSkippedFiles
// at any depth. destDir is created if it doesn't exist.
func CopyDirExcept(srcDir, destDir string, exclude ...string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && alwaysSkippedDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && alwaysSkippedFiles[d.Name()] {
			return nil
		}
		for _, skip := range exclude {
			if rel == skip {
				return nil
			}
		}

		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	return nil
}

// ZipDir packages srcDir as srcDir+".zip", with srcDir's base name as the
// single top-level folder inside the archive.
func ZipDir(srcDir string) (string, error) {
	return ZipDirTo(srcDir, srcDir+".zip", true)
}

// ZipDirTo packages srcDir as the archive zipPath. With withBase, srcDir's
// base name is the single top-level folder inside (the shape a single
// skill's .zip/.skill upload expects); without it, srcDir's children sit at
// the archive root (a bundle of several skill folders).
func ZipDirTo(srcDir, zipPath string, withBase bool) (string, error) {
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", zipPath, err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	base := filepath.Base(srcDir)
	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		entry := rel
		if withBase {
			entry = filepath.Join(base, rel)
		}
		w, err := zw.Create(filepath.ToSlash(entry))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("zipping %s: %w", srcDir, err)
	}
	return zipPath, nil
}
