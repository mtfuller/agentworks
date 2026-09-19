package filecopy

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Archives fetched from the internet are untrusted input, so extraction
// caps how much it will write regardless of what the archive claims.
const (
	maxArchiveEntries = 5000
	maxArchiveBytes   = 64 << 20 // 64 MiB
)

// Unzip extracts the zip archive at archivePath into destDir, which is
// created if it doesn't exist. Path-traversal entries (Zip Slip) and
// symlinks are rejected/skipped rather than followed; extracted files and
// directories never get an archive-supplied mode either: files are 0o644,
// or 0o755 if the archive marked them executable (see filePerm), and
// directories 0o755. setuid, setgid, sticky, and group/other-write bits are
// never carried over.
func Unzip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", archivePath, err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", destDir, err)
	}
	if len(r.File) > maxArchiveEntries {
		return fmt.Errorf("%s: too many entries (%d, max %d)", archivePath, len(r.File), maxArchiveEntries)
	}

	var total int64
	for _, f := range r.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return fmt.Errorf("%s: %w", archivePath, err)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", target, err)
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue // never follow an archive-supplied symlink
		}

		total += int64(f.UncompressedSize64)
		if total > maxArchiveBytes {
			return fmt.Errorf("%s: extracted content exceeds %d bytes", archivePath, maxArchiveBytes)
		}

		if err := extractZipEntry(f, target); err != nil {
			return fmt.Errorf("extracting %s: %w", f.Name, err)
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm(f.Mode()))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

// UntarGz extracts the gzip-compressed tar archive at archivePath into
// destDir, with the same Zip-Slip and symlink protections as Unzip. Only
// regular files and directories are extracted; symlinks, hardlinks, device
// files, and anything else are skipped.
func UntarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", archivePath, err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", destDir, err)
	}

	tr := tar.NewReader(gz)
	var entries int
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", archivePath, err)
		}

		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("%s: too many entries (max %d)", archivePath, maxArchiveEntries)
		}

		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return fmt.Errorf("%s: %w", archivePath, err)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", target, err)
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxArchiveBytes {
				return fmt.Errorf("%s: extracted content exceeds %d bytes", archivePath, maxArchiveBytes)
			}
			if err := extractTarEntry(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("extracting %s: %w", hdr.Name, err)
			}
		default:
			// Symlinks, hardlinks, devices, fifos, etc. -- never followed.
			continue
		}
	}
}

func extractTarEntry(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm(mode))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, r)
	return err
}

// filePerm is the permission an extracted or copied file gets: 0o755 if the
// source had any execute bit (scripts must stay runnable -- a hook or MCP
// server is often invoked directly), otherwise 0o644. Nothing else from the
// source's mode is trusted.
func filePerm(mode os.FileMode) os.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// safeJoin resolves name against destDir the way an archive entry's path is
// meant to be interpreted, rejecting anything that would land outside
// destDir (an absolute path, or a "../" escape -- the classic Zip Slip).
func safeJoin(destDir, name string) (string, error) {
	if filepath.IsAbs(name) || strings.HasPrefix(filepath.ToSlash(name), "/") {
		return "", fmt.Errorf("refusing absolute entry path %q", name)
	}
	target := filepath.Join(destDir, name)
	destWithSep := destDir + string(filepath.Separator)
	if target != destDir && !strings.HasPrefix(target, destWithSep) {
		return "", fmt.Errorf("refusing entry path %q escaping the destination", name)
	}
	return target, nil
}
