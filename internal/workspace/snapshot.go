package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	maxSnapshotEntries = 100_000
	maxHashBytes       = 8 << 20
	maxTotalHashBytes  = 256 << 20
)

type Snapshot struct {
	Files     map[string]FileState
	Truncated bool
}

type FileState struct {
	Mode       os.FileMode
	Size       int64
	ModifiedNS int64
	Digest     string
	LinkTarget string
}

type Changes struct {
	Added      []string `json:"added"`
	Modified   []string `json:"modified"`
	Deleted    []string `json:"deleted"`
	Truncated  bool     `json:"truncated"`
	Incomplete bool     `json:"incomplete"`
}

// Capture takes a bounded, symlink-safe snapshot. Small regular files are
// content-hashed; large files use size, timestamp, and mode metadata.
func Capture(root string) (Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, err
	}
	if canonical, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = canonical
	}
	snapshot := Snapshot{Files: map[string]FileState{}}
	var hashedBytes int64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			snapshot.Truncated = true
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && skippedDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if len(snapshot.Files) >= maxSnapshotEntries {
			snapshot.Truncated = true
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			snapshot.Truncated = true
			return nil
		}
		state := FileState{Mode: info.Mode(), Size: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
		if info.Mode()&os.ModeSymlink != 0 {
			state.LinkTarget, _ = os.Readlink(path)
		} else if info.Mode().IsRegular() && info.Size() <= maxHashBytes && hashedBytes+info.Size() <= maxTotalHashBytes {
			state.Digest, err = fileDigest(path)
			if err != nil {
				snapshot.Truncated = true
				state.Digest = ""
			} else {
				hashedBytes += info.Size()
			}
		} else if info.Mode().IsRegular() {
			snapshot.Truncated = true
		}
		snapshot.Files[filepath.ToSlash(relative)] = state
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot workspace: %w", err)
	}
	return snapshot, nil
}

func Compare(before, after Snapshot) Changes {
	changes := Changes{Added: []string{}, Modified: []string{}, Deleted: []string{},
		Truncated: before.Truncated || after.Truncated, Incomplete: before.Truncated || after.Truncated}
	for path, current := range after.Files {
		previous, existed := before.Files[path]
		if !existed {
			changes.Added = append(changes.Added, path)
		} else if previous != current {
			changes.Modified = append(changes.Modified, path)
		}
	}
	for path := range before.Files {
		if _, exists := after.Files[path]; !exists {
			changes.Deleted = append(changes.Deleted, path)
		}
	}
	sort.Strings(changes.Added)
	sort.Strings(changes.Modified)
	sort.Strings(changes.Deleted)
	return changes
}

func skippedDirectory(name string) bool {
	switch name {
	case ".git", ".agentworks", "node_modules", ".venv", "__pycache__":
		return true
	default:
		return false
	}
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
